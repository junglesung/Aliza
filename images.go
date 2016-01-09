package aliza

import (
	"appengine"
	"bytes"
	"image"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/pborman/uuid"
	"github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/tiff"
	"github.com/disintegration/imaging"

	gcscontext   "golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gcsappengine "google.golang.org/appengine"
	gcsfile      "google.golang.org/appengine/file"
	gcsurlfetch  "google.golang.org/appengine/urlfetch"
	"google.golang.org/cloud"
	"google.golang.org/cloud/storage"
)

func storeImage(rw http.ResponseWriter, req *http.Request) {
	// Appengine
	var c appengine.Context
	// Google Cloud Storage authentication
	var cc gcscontext.Context
	// Google Cloud Storage bucket name
	var bucket string = ""
	// User uploaded image file name
	var fileName string = uuid.New()
	// Transform user uploaded image to a thumbnail file name
	var fileNameThumbnail string = uuid.New()
	// User uploaded image file type
	var contentType string = ""
	// User uploaded image file raw data
	var b []byte
	// Google Cloud Storage file writer
	var wc *storage.Writer = nil
	var wct *storage.Writer = nil
	// Error
	var err error = nil
	// Result, 0: success, 1: failed
	var r int = 0

	// Set response in the end
	defer func() {
		// Return status. WriteHeader() must be called before call to Write
		if r == 0 {
			// Changing the header after a call to WriteHeader (or Write) has no effect.
			// rw.Header().Set("Location", req.URL.String()+"/"+cKey.Encode())
			rw.Header().Set("Location", "http://"+bucket+".storage.googleapis.com/"+fileName)
			rw.Header().Set("X-Thumbnail", "http://"+bucket+".storage.googleapis.com/"+ fileNameThumbnail)
			rw.WriteHeader(http.StatusCreated)
		} else {
			http.Error(rw, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		}
	}()

	// To log information in Google APP Engine console
	c = appengine.NewContext(req)

	// Get data from body
	b, err = ioutil.ReadAll(req.Body)
	if err != nil {
		c.Errorf("%s in reading body", err)
		r = 1
		return
	}
	c.Infof("Body length %d bytes, read %d bytes", req.ContentLength, len(b))

	// Determine filename extension from content type
	contentType = req.Header["Content-Type"][0]
	switch contentType {
	case "image/jpeg":
		fileName += ".jpg"
		fileNameThumbnail += ".jpg"
	default:
		c.Errorf("Unknown or unsupported content type '%s'. Valid: image/jpeg", contentType)
		r = 1
		return
	}
	c.Infof("Content type %s is received, %s is detected.", contentType, http.DetectContentType(b))

	// Get default bucket name
	cc = gcsappengine.NewContext(req)
	if bucket, err = gcsfile.DefaultBucketName(cc); err != nil {
		c.Errorf("%s in getting default GCS bucket name", err)
		r = 1
		return
	}
	c.Infof("APP Engine Version: %s", gcsappengine.VersionID(cc))
	c.Infof("Using bucket name: %s", bucket)

	// Prepare Google Cloud Storage authentication
	hc := &http.Client{
		Transport: &oauth2.Transport{
			Source: google.AppEngineTokenSource(cc, storage.ScopeFullControl),
			// Note that the App Engine urlfetch service has a limit of 10MB uploads and
			// 32MB downloads.
			// See https://cloud.google.com/appengine/docs/go/urlfetch/#Go_Quotas_and_limits
			// for more information.
			Base: &gcsurlfetch.Transport{Context: cc},
		},
	}
	ctx := cloud.NewContext(gcsappengine.AppID(cc), hc)

	// Change default object ACLs
	err = storage.PutDefaultACLRule(ctx, bucket, "allUsers", storage.RoleReader)
	// err = storage.PutACLRule(ctx, bucket, fileName, "allUsers", storage.RoleReader)
	if err != nil {
		c.Errorf("%v in saving ACL rule for bucket %q", err, bucket)
		return
	}

	// Store file in Google Cloud Storage
	wc = storage.NewWriter(ctx, bucket, fileName)
	wc.ContentType = contentType
	wct = storage.NewWriter(ctx, bucket, fileNameThumbnail)
	wct.ContentType = contentType
	// wc.Metadata = map[string]string{
	// 	"x-goog-meta-foo": "foo",
	// 	"x-goog-meta-bar": "bar",
	// }
	if err = processAndSaveJpeg(c, bytes.NewReader(b), wc, wct); err != nil {
		c.Errorf("Process image and save to GCS failed")
		r = 1
		return
	}
	if err = wc.Close(); err != nil {
		c.Errorf("CreateFile: unable to close bucket %q, file %q: %v", bucket, fileName, err)
		r = 1
		return
	}
	if err = wct.Close(); err != nil {
		c.Errorf("CreateFileThumbnail: unable to close bucket %q, file %q: %v", bucket, fileNameThumbnail, err)
		r = 1
		return
	}
	c.Infof("/%v/%v, /%v/%v created", bucket, fileName, bucket, fileNameThumbnail)
}

// Rotate a JPEG image according to its EXIF orientation
// Also create an image thumbnail
// Save to 2 files
func processAndSaveJpeg(c appengine.Context, in *bytes.Reader, outRotated io.Writer, outThumbnail io.Writer) (err error) {
	var x *exif.Exif = nil
	var orientation *tiff.Tag = nil
	var openImage image.Image
	var rotateImage *image.NRGBA = nil
	var smallImage *image.NRGBA = nil
	err = nil

	// Read EXIF
	if _, err = in.Seek(0, 0); err != nil {
		c.Errorf("%s in moving the reader offset to the beginning in order to read EXIF", err)
		return
	}
	if x, err = exif.Decode(in); err != nil {
		c.Errorf("%s in decoding JPEG image", err)
		return
	}

	// Get Orientation
	if orientation, err = x.Get(exif.Orientation); err != nil {
		c.Warningf("%s in getting orientation from EXIF", err)
		return
	}
	c.Debugf("Orientation %s", orientation.String())

	// Open image
	if _, err = in.Seek(0, 0); err != nil {
		c.Errorf("%s in moving the reader offset to the beginning in order to read EXIF", err)
		return
	}
	if openImage, err = imaging.Decode(in); err != nil {
		c.Errorf("%s in opening image %s", err)
		return
	}

	switch orientation.String() {
	case "1":
	// Do nothing
	case "2":
		rotateImage = imaging.FlipH(openImage)
	case "3":
		rotateImage = imaging.Rotate180(openImage)
	case "4":
		rotateImage = imaging.FlipV(openImage)
	case "5":
		rotateImage = imaging.Transverse(openImage)
	case "6":
		rotateImage = imaging.Rotate270(openImage)
	case "7":
		rotateImage = imaging.Transpose(openImage)
	case "8":
		rotateImage = imaging.Rotate90(openImage)
	}

	// Small
	if rotateImage.Rect.Dx() > rotateImage.Rect.Dy() {
		smallImage = imaging.Resize(rotateImage, 1920, 0, imaging.Lanczos)
	} else {
		smallImage = imaging.Resize(rotateImage, 0, 1920, imaging.Lanczos)
	}

	// Save
	if outRotated != nil {
		if err = imaging.Encode(outRotated, rotateImage, imaging.JPEG); err != nil {
			c.Errorf("%s in saving rotated image", err)
			return
		}
	}
	if outThumbnail != nil {
		if imaging.Encode(outThumbnail, smallImage, imaging.JPEG); err != nil {
			c.Errorf("%s in saving image thumbnail", err)
			return
		}
	}

	return
}

