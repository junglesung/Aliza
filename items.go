package aliza

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
)

type ItemMember struct {
	UserKey string
	Attendant int
}

type Item struct {
	Id           string     `json:"id"          datastore:"-"`
	Image        string     `json:"image"`
	People       int        `json:"people"`
	Attendant    int        `json:"attendant"`
	Latitude     float64    `json:"latitude"`
	Longitude    float64    `json:"longitude"`
	CreateTime   time.Time  `json:"createtime"`
	// Members are whom join this item. The first member is the item owner.
	// When the first member leaves, delete the item.
	Member     []ItemMember `json:"member"`
}

const ItemKind = "Item"
const ItemRoot = "Item Root"

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

	// Set now as the creation time. Precision to a second.
	item.CreateTime = time.Unix(time.Now().Unix(), 0)

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
	// To log messages
	c := appengine.NewContext(req)

	if len(req.URL.Query()) == 0 {
		// Get key from URL
		tokens := strings.Split(req.URL.Path, "/")
		var keyIndexInTokens int = 0
		for i, v := range tokens {
			if v == "items" {
				keyIndexInTokens = i + 1
			}
		}
		if keyIndexInTokens >= len(tokens) {
			c.Debugf("Key is not given so that query all items")
			queryAllItem(rw, req)
			return
		}
		keyString := tokens[keyIndexInTokens]
		if keyString == "" {
			c.Debugf("Key is empty so that query all items")
			queryAllItem(rw, req)
		} else {
			queryOneItem(rw, req, keyString)
		}
	} else {
		searchItem(rw, req)
	}
}

func queryAllItem(rw http.ResponseWriter, req *http.Request) {
	// To access datastore and to log
	c := appengine.NewContext(req)
	c.Debugf("QueryAll()")

	// Get all entities
	var dst []Item
	r := 0
	k, err := datastore.NewQuery(ItemKind).Order("-CreateTime").GetAll(c, &dst)
	if err != nil {
		c.Errorf("%s", err)
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
		c.Errorf("%s in encoding result %v", err, dst)
	} else {
		c.Infof("QueryAll() returns %d items", len(dst))
	}
}

func queryOneItem(rw http.ResponseWriter, req *http.Request, keyString string) {
	// To access datastore and to log
	c := appengine.NewContext(req)
	c.Debugf("QueryOneItem()")

	// Entity
	var dst Item
	// Result
	r := http.StatusOK

	defer func() {
		// Return status. WriteHeader() must be called before call to Write
		if r == http.StatusOK {
			rw.WriteHeader(http.StatusOK)
			// Return body
			encoder := json.NewEncoder(rw)
			if err := encoder.Encode(dst); err != nil {
				c.Errorf("%s in encoding result %v", err, dst)
			}
		} else {
			http.Error(rw, http.StatusText(r), r)
		}
	}()

	// Decode key from string
	key, err := datastore.DecodeKey(keyString)
	if err != nil {
		c.Errorf("%s in decoding key string", err)
		r = http.StatusBadRequest
		return
	}

	// Get the entity
	if err := datastore.Get(c, key, &dst); err != nil {
		c.Errorf("%s in getting entity from datastore by key %s", err, keyString)
		r = http.StatusNotFound
		return
	}

	// Store key to item
	dst.Id = keyString

	// Vernon debug
	c.Debugf("Got item %v", dst)
	b, err := json.Marshal(dst)
	if err != nil {
		c.Errorf("%s in marshaling item %v", err, dst)
	} else {
		c.Debugf("Item JSON %s", b)
	}
	// GOTO defer()
	return
}

func searchItem(rw http.ResponseWriter, req *http.Request) {
	// Appengine
	var c appengine.Context = appengine.NewContext(req)
	c.Debugf("searchItem()")

	// Get all entities
	var dst []Item
	// Error flag
	r := 0
	// Query
	q := req.URL.Query()
	f := datastore.NewQuery(ItemKind)

	for key := range q {
		switch key {
		case "Image":  // string
			var v string = q.Get(key)
			f = f.Filter(key+"=", v)
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
		case "Latitude":  // float64
			v, err := strconv.ParseFloat(q.Get(key), 64)
			if err != nil {
				c.Errorf("%s in converting %s number\n", err, key)
			} else {
				f = f.Filter(key+"=", v)
			}
		case "Longitude":  // float64
			v, err := strconv.ParseFloat(q.Get(key), 64)
			if err != nil {
				c.Errorf("%s in converting %s number\n", err, key)
			} else {
				f = f.Filter(key+"=", v)
			}
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

func updateItem(rw http.ResponseWriter, req *http.Request) {
	// To access datastore and to log
	c := appengine.NewContext(req)
	c.Debugf("UpdateItem()")
	// Result
	r := http.StatusOK
	// Item
	var src Item

	// Set response
	defer func() {
		// Return status. WriteHeader() must be called before call to Write
		if r == http.StatusOK {
			rw.WriteHeader(http.StatusOK)
		} else {
			http.Error(rw, http.StatusText(r), r)
		}
	}()

	// Get key from URL
	tokens := strings.Split(req.URL.Path, "/")
	var keyIndexInTokens int = 0
	for i, v := range tokens {
		if v == "items" {
			keyIndexInTokens = i + 1
		}
	}
	if keyIndexInTokens >= len(tokens) {
		c.Debugf("Key is not given")
		r = http.StatusBadRequest
		return
	}
	keyString := tokens[keyIndexInTokens]
	if keyString == "" {
		c.Debugf("Key is empty")
		r = http.StatusBadRequest
		return
	}

	// Decode key from string
	key, err := datastore.DecodeKey(keyString)
	if err != nil {
		c.Errorf("%s in decoding key string", err)
		r = http.StatusBadRequest
		return
	}

	// Get data from body
	b, err := ioutil.ReadAll(req.Body)
	if err != nil {
		c.Errorf("%s in reading body %s", err, b)
		r = http.StatusBadRequest
		return
	}
	if err = json.Unmarshal(b, &src); err != nil {
		c.Errorf("%s in decoding body %s", err, b)
		r = http.StatusBadRequest
		return
	}

	// Update in a transaction
	err = datastore.RunInTransaction(c, func(c appengine.Context) error {
		var err1 error
		r, err1 = updateOneItemInDatastore(c, key, &src)
		return err1
	}, nil)

	// GOTO defer()
	return
}

func updateOneItemInDatastore(c appengine.Context, key *datastore.Key, src *Item) (r int, err error) {
	var dst Item
	// Get the entity
	if err = datastore.Get(c, key, &dst); err != nil {
		c.Errorf("%s in getting entity from datastore by key %s", err, key.Encode())
		r = http.StatusNotFound
		return
	}

	// Vernon debug
	c.Debugf("Got from user %v", src)
	c.Debugf("Got from server %v", dst)

	// Update CreateTime when owner update information
	// Don't update CreateTime when people attend or leave
	var toUpdateTime bool = false
	// Change values
	if (src.Image != "") {
		dst.Image = src.Image
		toUpdateTime = true
	}
	if (src.People != 0) {
		dst.People = src.People
		toUpdateTime = true
	}
	if (src.Attendant != 0) {
		dst.Attendant += src.Attendant
	}
	// Set now as the creation time. Precision to a second.
	if toUpdateTime == true {
		dst.CreateTime = time.Unix(time.Now().Unix(), 0)
	}
	// Don't update Latitude and Longitude because owner can update anywhere away from the shop

	// Vernon debug
	c.Debugf("Update server %v", dst)

	// Update to server
	b, err := json.Marshal(dst)
	if err != nil {
		c.Errorf("%s in marshaling item %v", err, dst)
		r = http.StatusBadRequest
		return
	}

	// Vernon debug
	c.Debugf("Convert to JSON %s", b)

	// Store item into datastore
	_, err = datastore.Put(c, key, &dst)
	if err != nil {
		c.Errorf("%s in storing in datastore with key %s", err, key.Encode())
		r = http.StatusNotFound
		return
	}

	return
}

func deleteItem(rw http.ResponseWriter, req *http.Request) {
	// To log
	c := appengine.NewContext(req)
	// Get key from URL
	tokens := strings.Split(req.URL.Path, "/")
	var keyIndexInTokens int = 0
	for i, v := range tokens {
		if v == "items" {
			keyIndexInTokens = i + 1
		}
	}
	if keyIndexInTokens >= len(tokens) {
		c.Infof("Key is not given so that delete all items")
		deleteAllItem(rw, req)
		return
	}
	keyString := tokens[keyIndexInTokens]
	if keyString == "" {
		c.Infof("Key is empty so that delete all items")
		deleteAllItem(rw, req)
	} else {
		deleteOneItem(rw, req, keyString)
	}
}

func deleteAllItem(rw http.ResponseWriter, req *http.Request) {
	// To access datastore and to log
	c := appengine.NewContext(req)
	c.Infof("deleteAll()")

	// Delete root entity after other entities
	r := 0
	pKey := datastore.NewKey(c, ItemKind, ItemRoot, 0, nil)
	if keys, err := datastore.NewQuery(ItemKind).KeysOnly().GetAll(c, nil); err != nil {
		c.Errorf("%s", err)
		r = 1
	} else if err := datastore.DeleteMulti(c, keys); err != nil {
		c.Errorf("%s", err)
		r = 1
	} else if err := datastore.Delete(c, pKey); err != nil {
		c.Errorf("%s", err)
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
	// To access datastore and to log
	c := appengine.NewContext(req)
	c.Infof("deleteOneItem()")

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
		c.Errorf("%s in decoding key string", err)
		r = http.StatusBadRequest
		return
	}

	// Delete the entity
	if err := datastore.Delete(c, key); err != nil {
		c.Errorf("%s, in deleting entity by key", err)
		r = http.StatusNotFound
		return
	}
	c.Infof("Key %s is deleted", keyString)
}

