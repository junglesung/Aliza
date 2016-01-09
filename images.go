package aliza

import (
	"appengine"
	"bytes"
	"image"
	"io/ioutil"
	"net/http"

	"github.com/pborman/uuid"
	"github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/tiff"
	"github.com/disintegration/imaging"

	gcscontext   "golang.org/x/net/context"
	gcsappengine "google.golang.org/appengine"
	gcsfile      "google.golang.org/appengine/file"
	"google.golang.org/cloud/storage"
)

func storeImage(rw http.ResponseWriter, req *http.Request) {
	// Appengine
	var c appengine.Context
	// Google Cloud Storage authentication
	var cc gcscontext.Context
	// Google Cloud Storage bucket name
	var bucketName string = ""
	// Google Cloud Storage client
	var client *storage.Client
	// Google Cloud Storage bucket
	var bucketHandle *storage.BucketHandle
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
	// Error
	var err error = nil
	// Result, 0: success, 1: failed
	var r int = http.StatusCreated

	// Set response in the end
	defer func() {
		// Return status. WriteHeader() must be called before call to Write
		if r == http.StatusCreated {
			// Changing the header after a call to WriteHeader (or Write) has no effect.
			// rw.Header().Set("Location", req.URL.String()+"/"+cKey.Encode())
			rw.Header().Set("Location", "http://"+ bucketName +".storage.googleapis.com/"+fileName)
			rw.Header().Set("X-Thumbnail", "http://"+ bucketName +".storage.googleapis.com/"+ fileNameThumbnail)
			rw.WriteHeader(r)
		} else {
			http.Error(rw, http.StatusText(r), r)
		}
	}()

	// To log information in Google APP Engine console
	c = appengine.NewContext(req)

	// Get data from body
	b, err = ioutil.ReadAll(req.Body)
	if err != nil {
		c.Errorf("%s in reading body", err)
		r = http.StatusInternalServerError
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
		r = http.StatusBadRequest
		return
	}
	c.Infof("Content type %s is received, %s is detected.", contentType, http.DetectContentType(b))

	// Prepare Google Cloud Storage authentication
	cc = gcsappengine.NewContext(req)
	if client, err = storage.NewClient(cc); err != nil {
		c.Errorf("%s in initializing a GCS client", err)
		r = http.StatusInternalServerError
		return
	}
	defer client.Close()

	// Get default bucket
	if bucketName, err = gcsfile.DefaultBucketName(cc); err != nil {
		c.Errorf("%s in getting default GCS bucket name", err)
		r = http.StatusInternalServerError
		return
	}
	bucketHandle = client.Bucket(bucketName)
	c.Infof("APP Engine Version: %s", gcsappengine.VersionID(cc))
	c.Infof("Using bucket name: %s", bucketName)

	// Change default object ACLs
	if err = bucketHandle.DefaultObjectACL().Set(cc, storage.AllUsers, storage.RoleReader); err != nil {
		c.Errorf("%v in saving default object ACL rule for bucket %q", err, bucketName)
		r = http.StatusInternalServerError
		return
	}

	// Store rotated image in Google Cloud Storage
	var in *bytes.Reader = bytes.NewReader(b)
	var x *exif.Exif = nil
	var orientation *tiff.Tag = nil
	var beforeImage image.Image
	var afterImage *image.NRGBA = nil

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
	if beforeImage, err = imaging.Decode(in); err != nil {
		c.Errorf("%s in opening image %s", err)
		return
	}

	switch orientation.String() {
	case "1":
		afterImage = beforeImage.(*image.NRGBA)
	case "2":
		afterImage = imaging.FlipH(beforeImage)
	case "3":
		afterImage = imaging.Rotate180(beforeImage)
	case "4":
		afterImage = imaging.FlipV(beforeImage)
	case "5":
		afterImage = imaging.Transverse(beforeImage)
	case "6":
		afterImage = imaging.Rotate270(beforeImage)
	case "7":
		afterImage = imaging.Transpose(beforeImage)
	case "8":
		afterImage = imaging.Rotate90(beforeImage)
	}

	// Save rotated image
	wc = bucketHandle.Object(fileName).NewWriter(cc)
	wc.ContentType = contentType
	if err = imaging.Encode(wc, afterImage, imaging.JPEG); err != nil {
		c.Errorf("%s in saving rotated image", err)
		return
	}
	if err = wc.Close(); err != nil {
		c.Errorf("CreateFile: unable to close bucket %q, file %q: %v", bucketName, fileName, err)
		r = 1
		return
	}
	wc = nil

	// Make thumbnail
	if afterImage.Rect.Dx() > afterImage.Rect.Dy() {
		afterImage = imaging.Resize(afterImage, 1920, 0, imaging.Lanczos)
	} else {
		afterImage = imaging.Resize(afterImage, 0, 1920, imaging.Lanczos)
	}

	// Save thumbnail
	wc = bucketHandle.Object(fileNameThumbnail).NewWriter(cc)
	wc.ContentType = contentType
	if imaging.Encode(wc, afterImage, imaging.JPEG); err != nil {
		c.Errorf("%s in saving image thumbnail", err)
		return
	}
	if err = wc.Close(); err != nil {
		c.Errorf("CreateFileThumbnail: unable to close bucket %q, file %q: %v", bucketName, fileNameThumbnail, err)
		r = 1
		return
	}

	c.Infof("/%v/%v, /%v/%v created", bucketName, fileName, bucketName, fileNameThumbnail)
}
