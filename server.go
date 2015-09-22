package main

import (
	"appengine"
	"appengine/datastore"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pborman/uuid"
	gcscontext   "golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gcsappengine "google.golang.org/appengine"
	gcsfile      "google.golang.org/appengine/file"
	gcsurlfetch  "google.golang.org/appengine/urlfetch"
	"google.golang.org/cloud"
	"google.golang.org/cloud/storage"
)

type Item struct {
	Id         string    `json:"id"          datastore:"-"`
	People     int       `json:"people"`
	Attendant  int       `json:"attendant"`
	Image      string    `json:"image"`
	CreateTime time.Time `json:"createtime"`
}

const BaseUrl = "/api/0.1/"
const ItemKind = "Item"
const ItemRoot = "Item Root"

func init() {
	http.HandleFunc(BaseUrl, rootPage)
	http.HandleFunc(BaseUrl+"queryAll", queryAll)
	http.HandleFunc(BaseUrl+"storeImage", storeImage)
	http.HandleFunc(BaseUrl+"deleteAll", deleteAll)
	http.HandleFunc(BaseUrl+"images", images)
	http.HandleFunc(BaseUrl+"items", items)
}

func rootPage(rw http.ResponseWriter, req *http.Request) {
	//
}

func images(rw http.ResponseWriter, req *http.Request) {
	switch req.Method {
	// case "GET":
		// queryImage(rw, req)
	case "POST":
		storeImage(rw, req)
	// case "DELETE":
	// 	deleteImage(rw, req)
	default:
		// queryAllImage(rw, req)
	}
}

func items(rw http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		queryItem(rw, req)
	case "POST":
		storeItem(rw, req)
	case "DELETE":
		deleteItem(rw, req)
	default:
		queryAll(rw, req)
	}
}

func storeImage(rw http.ResponseWriter, req *http.Request) {
	// Appengine
	var c appengine.Context
	// Google Cloud Storage authentication
	var cc gcscontext.Context
	// Google Cloud Storage bucket name
	var bucket string = ""
	// User uploaded image file name
	var fileName string = uuid.New()
	// User uploaded image file type
	var contentType string = ""
	// User uploaded image file raw data
	var b []byte
	// Result, 0: success, 1: failed
	var r int = 0

	// Set response in the end
	defer func() {
		// Return status. WriteHeader() must be called before call to Write
		if r == 0 {
			// Changing the header after a call to WriteHeader (or Write) has no effect.
			// rw.Header().Set("Location", req.URL.String()+"/"+cKey.Encode())
			rw.Header().Set("Location", "http://"+bucket+".storage.googleapis.com/"+fileName)
			rw.WriteHeader(http.StatusCreated)
		} else {
			http.Error(rw, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		}
	}()

	// To log information in Google APP Engine console
	c = appengine.NewContext(req)

	// Get data from body
	b, err := ioutil.ReadAll(req.Body)
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
	wc := storage.NewWriter(ctx, bucket, fileName)
	wc.ContentType = contentType
	// wc.Metadata = map[string]string{
	// 	"x-goog-meta-foo": "foo",
	// 	"x-goog-meta-bar": "bar",
	// }
	if _, err := wc.Write(b); err != nil {
		c.Errorf("CreateFile: unable to write data to bucket %q, file %q: %v", bucket, fileName, err)
		r = 1
		return
	}
	if err := wc.Close(); err != nil {
		c.Errorf("CreateFile: unable to close bucket %q, file %q: %v", bucket, fileName, err)
		r = 1
		return
	}
	c.Infof("/%v/%v\n created", bucket, fileName)
}

func storeItem(rw http.ResponseWriter, req *http.Request) {
	// Appengine
	var c appengine.Context = appengine.NewContext(req)
	// Result, 0: success, 1: failed
	var r int = 0
	var cKey *datastore.Key = nil
	defer func() {
		// Return status. WriteHeader() must be called before call to Write
		if r == 0 {
			// Changing the header after a call to WriteHeader (or Write) has no effect.
			rw.Header().Set("Location", req.URL.String()+"/"+cKey.Encode())
			rw.WriteHeader(http.StatusCreated)
		} else {
			http.Error(rw, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		}
	}()

	// Get data from body
	b, err := ioutil.ReadAll(req.Body)
	if err != nil {
		c.Errorf("%s in reading body %s", err, b)
		r = 1
		return
	}
	var item Item
	if err = json.Unmarshal(b, &item); err != nil {
		c.Errorf("%s in decoding body %s", err, b)
		r = 1
		return
	}

	// Set now as the creation time
	item.CreateTime = time.Now()

	// Vernon debug
	c.Debugf("Store item %s", b)

	// Store item into datastore
	pKey := datastore.NewKey(c, ItemKind, ItemRoot, 0, nil)
	cKey, err = datastore.Put(c, datastore.NewIncompleteKey(c, ItemKind, pKey), &item)
	if err != nil {
		c.Errorf("%s in storing in datastore", err)
		log.Println(err)
		r = 1
		return
	}
}

func queryItem(rw http.ResponseWriter, req *http.Request) {
	if len(req.URL.Query()) == 0 {
		queryAll(rw, req)
	} else {
		searchItem(rw, req)
	}
}

func queryAll(rw http.ResponseWriter, req *http.Request) {
	// Get all entities
	var dst []Item
	r := 0
	c := appengine.NewContext(req)
	k, err := datastore.NewQuery(ItemKind).Order("-CreateTime").GetAll(c, &dst)
	if err != nil {
		log.Println(err)
		r = 1
	}

	// Map keys and items
	for i, v := range k {
		dst[i].Id = v.Encode()
	}

	// Return status. WriteHeader() must be called before call to Write
	if r == 0 {
		rw.WriteHeader(http.StatusOK)
	} else {
		http.Error(rw, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	// Return body
	encoder := json.NewEncoder(rw)
	if err = encoder.Encode(dst); err != nil {
		log.Println(err, "in encoding result", dst)
	} else {
		log.Printf("QueryAll() returns %d items\n", len(dst))
	}
}

func searchItem(rw http.ResponseWriter, req *http.Request) {
	// Appengine
	var c appengine.Context = appengine.NewContext(req)
	// Get all entities
	var dst []Item
	// Error flag
	r := 0
	// Query
	q := req.URL.Query()
	f := datastore.NewQuery(ItemKind)
	for key := range q {
		switch key {
		case "People":  // int
			v, err := strconv.Atoi(q.Get(key))
			if err != nil {
				c.Errorf("%s in converting %s number\n", err, key)
			} else {
				f = f.Filter(key+"=", v)
			}
		case "Attendant":  // int
			v, err := strconv.Atoi(q.Get(key))
			if err != nil {
				c.Errorf("%s in converting %s number\n", err, key)
			} else {
				f = f.Filter(key+"=", v)
			}
		case "Image":  // string
			var v string = q.Get(key)
			f = f.Filter(key+"=", v)
		case "CreateTime":  // time.Time
			// TODO: define the format of time so that time.Parse() can correctly parse
			var tmp string = q.Get(key)
			v, err := time.Parse(tmp, tmp)
			if err != nil {
				c.Errorf("%s in converting %s number\n", err, key)
			} else {
				f = f.Filter(key+"=", v)
			}
			// Vernon debug
			c.Debugf("%s, %s\n", key, v)
		default:
			c.Infof("%s is a wrong query property\n", key)
		}
	}
	k, err := f.GetAll(c, &dst)
	if err != nil {
		c.Errorf("%s in getting data from datastore\n", err)
		r = 1
	}

	// Map keys and items
	for i, v := range k {
		dst[i].Id = v.Encode()
	}

	// Return status. WriteHeader() must be called before call to Write
	if r == 0 {
		rw.WriteHeader(http.StatusOK)
	} else {
		http.Error(rw, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	// Return body
	encoder := json.NewEncoder(rw)
	if err = encoder.Encode(dst); err != nil {
		log.Println(err, "in encoding result", dst)
	} else {
		log.Printf("SearchItem() returns %d items\n", len(dst))
	}
}

func deleteItem(rw http.ResponseWriter, req *http.Request) {
	// Get key from URL
	tokens := strings.Split(req.URL.Path, "/")
	var keyIndexInTokens int = 0
	for i, v := range tokens {
		if v == "items" {
			keyIndexInTokens = i + 1
		}
	}
	if keyIndexInTokens >= len(tokens) {
		log.Println("Key is not given so that delete all items")
		deleteAll(rw, req)
		return
	}
	keyString := tokens[keyIndexInTokens]
	if keyString == "" {
		log.Println("Key is empty so that delete all items")
		deleteAll(rw, req)
	} else {
		deleteOneItem(rw, req, keyString)
	}
}

func deleteAll(rw http.ResponseWriter, req *http.Request) {
	// Delete root entity after other entities
	r := 0
	c := appengine.NewContext(req)
	pKey := datastore.NewKey(c, ItemKind, ItemRoot, 0, nil)
	if keys, err := datastore.NewQuery(ItemKind).KeysOnly().GetAll(c, nil); err != nil {
		log.Println(err)
		r = 1
	} else if err := datastore.DeleteMulti(c, keys); err != nil {
		log.Println(err)
		r = 1
	} else if err := datastore.Delete(c, pKey); err != nil {
		log.Println(err)
		r = 1
	}

	// Return status. WriteHeader() must be called before call to Write
	if r == 0 {
		rw.WriteHeader(http.StatusOK)
	} else {
		http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func deleteOneItem(rw http.ResponseWriter, req *http.Request, keyString string) {
	// Result
	r := http.StatusNoContent
	defer func() {
		// Return status. WriteHeader() must be called before call to Write
		if r == http.StatusNoContent {
			rw.WriteHeader(http.StatusNoContent)
		} else {
			http.Error(rw, http.StatusText(r), r)
		}
	}()

	key, err := datastore.DecodeKey(keyString)
	if err != nil {
		log.Println(err, "in decoding key string")
		r = http.StatusBadRequest
		return
	}

	// Delete the entity
	c := appengine.NewContext(req)
	if err := datastore.Delete(c, key); err != nil {
		log.Println(err, "in deleting entity by key")
		r = http.StatusNotFound
		return
	}
	log.Println(key, "is deleted")
}
