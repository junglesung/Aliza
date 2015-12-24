package aliza

import (
	"appengine"
	"appengine/datastore"
	"appengine/urlfetch"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"fmt"
)

type ItemMember struct {
	UserKey      string     `json:"userkey"`
	Attendant    int        `json:"attendant"`
}

type Item struct {
	Id             string     `json:"id"          datastore:"-"`
	Image          string     `json:"image"`
	People         int        `json:"people"`
	Attendant      int        `json:"attendant"`
	Latitude       float64    `json:"latitude"`
	Longitude      float64    `json:"longitude"`
	CreateTime     time.Time  `json:"createtime"`
	// Members are whom join this item. The first member is the item owner.
	// When the first member leaves, delete the item.
	Members      []ItemMember `json:"members"`
	// Google Cloud Messaging group unique name and ID. Reference: https://developers.google.com/cloud-messaging/notifications
	GcmGroupName   string     `json:"gcmgroupname"`
	GcmGroupKey    string     `json:"gcmgroupkey"`
}

type ItemUpdateNotification struct {
	Message       string `json:"message"`
	ItemId        string `json:"itemid"`
	RequestUserId string `json:"requestuserid"`
}

const ItemKind = "Item"
const ItemRoot = "Item Root"

// Update item state
type UpdateItemState int
const (
	stateAppendMember UpdateItemState = iota
	stateAddAttendant
	stateDeleteMember
	stateDeleteItem
	stateLast         // Dummy state to indicate the enum end
)

func storeItem(rw http.ResponseWriter, req *http.Request) {
	// Appengine
	var c appengine.Context = appengine.NewContext(req)
	// Result, 0: success, 1: failed
	var r int = http.StatusCreated
	var cKey *datastore.Key = nil

	// Write response finally
	defer func() {
		// Return status. WriteHeader() must be called before call to Write
		if r == http.StatusCreated {
			// Changing the header after a call to WriteHeader (or Write) has no effect.
			rw.Header().Set("Location", req.URL.String()+"/"+cKey.Encode())
			rw.WriteHeader(http.StatusCreated)
		} else {
			// http.StatusBadRequest
			http.Error(rw, http.StatusText(r), r)
		}
	}()

	// Get data from body
	b, err := ioutil.ReadAll(req.Body)
	if err != nil {
		c.Errorf("%s in reading body %s", err, b)
		r = http.StatusBadRequest
		return
	}
	var item Item
	if err = json.Unmarshal(b, &item); err != nil {
		c.Errorf("%s in decoding body %s", err, b)
		r = http.StatusBadRequest
		return
	}

	// Verify data
	if item.Image == "" {
		c.Errorf("The request does not specify item image URL")
		r = http.StatusBadRequest
		return
	}
	if item.Attendant <= 0 {
		c.Errorf("Item attendant %d must be >= 0", item.Attendant)
		r = http.StatusBadRequest
		return
	}
	if item.Attendant >= item.People {
		c.Errorf("Item attendant %d can't be greater or equal to item people %d", item.Attendant, item.People)
		r = http.StatusBadRequest
		return
	}
	if item.Latitude < -90 || item.Latitude > 90 {
		c.Errorf("Latitude %d should be -90~90", item.Latitude)
		r = http.StatusBadRequest
		return
	}
	if item.Longitude < -180 || item.Longitude > 180 {
		c.Errorf("Latitude %d should be -180~180", item.Longitude)
		r = http.StatusBadRequest
		return
	}

	// Set the first member as owner to the user key
	var pUser    *User
	var pUserKey *datastore.Key
	var instanceId string = req.Header.Get(HttpHeaderInstanceId)
	if pUserKey, pUser, err = searchUser(instanceId, c); err != nil {
		c.Errorf("%s in searching user %v", err, instanceId)
		r = http.StatusInternalServerError
		return
	}
	item.Members = make([]ItemMember, 1)
	item.Members[0].UserKey = pUserKey.Encode()
	item.Members[0].Attendant = item.Attendant

	// Set now as the creation time. Precision to a second.
	item.CreateTime = time.Unix(time.Now().Unix(), 0)

	// Set GCM group name
	item.GcmGroupName = pUserKey.Encode() + strconv.FormatInt(item.CreateTime.UnixNano(), 16)

	// Vernon debug
	c.Debugf("Create a GCM group...")

	// Create a new GCM group with the owner
	var operation GroupOperation
	var gcmResponseCode int
	// Create a new group on GCM server with the name of owner's user key
	operation.Operation = "create"
	operation.Notification_key_name = item.GcmGroupName
	operation.Registration_ids = []string{pUser.RegistrationToken}
	if gcmResponseCode = sendGroupOperationToGcm(&operation, c); gcmResponseCode != http.StatusOK {
		c.Errorf("Send group operation to GCM failed")
		r = gcmResponseCode
		return
	}
	c.Infof("GCM group %s is created", item.GcmGroupName)

	// Set GCM group key
	item.GcmGroupKey = operation.Notification_key

	// Vernon debug
	c.Debugf("Store item %+v", item)

	// Store item into datastore
	pKey := datastore.NewKey(c, ItemKind, ItemRoot, 0, nil)
	cKey, err = datastore.Put(c, datastore.NewIncompleteKey(c, ItemKind, pKey), &item)
	if err != nil {
		c.Errorf("%s in storing in datastore", err)
		log.Println(err)
		r = http.StatusInternalServerError
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

		// Vernon debug
		c.Debugf("Item from datastore %+v", dst[i])
	}

	// Return status. WriteHeader() must be called before call to Write
	if r == 0 {
		rw.WriteHeader(http.StatusOK)
	} else {
		http.Error(rw, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	// Vernon debug
	b, _ := json.Marshal(dst)
	c.Debugf("Item JSON %s", b)

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

	// Organize data
	var pUser    *User
	var pKeyUser *datastore.Key
	var instanceId string = req.Header.Get(HttpHeaderInstanceId)
	if pKeyUser, pUser, err = searchUser(instanceId, c); err != nil {
		c.Errorf("%s in searching user instance ID %s", err, instanceId)
		r = http.StatusInternalServerError
		return
	}
	if pKeyUser == nil {
		c.Errorf("User instance ID %s is not found", instanceId)
		r = http.StatusInternalServerError
		return
	}
	src.Members = make([]ItemMember, 1)
	src.Members[0].UserKey = pKeyUser.Encode()
	src.Members[0].Attendant = src.Attendant

	// Update in a transaction
	err = datastore.RunInTransaction(c, func(c appengine.Context) error {
		var err1 error
		r, err1 = updateOneItemInDatastore(c, key, &src, pUser, pKeyUser)
		return err1
	}, nil)

	// GOTO defer()
	return
}

func updateOneItemInDatastore(c                appengine.Context,
                              key             *datastore.Key,
                              src             *Item,
                              pRequestUser    *User,
                              pRequestUserKey *datastore.Key) (r int, err error) {
	// Existing item got from datastore
	var dst Item
	// Update state
	var state UpdateItemState = stateLast
	// Response code received from GCM server
	var gcmResponseCode int
	// Notification
	var notification ItemUpdateNotification

	// Initial variables
	r = http.StatusOK
	err = nil

	// Get the entity
	if err = datastore.Get(c, key, &dst); err != nil {
		c.Errorf("%s in getting entity from datastore by key %s", err, key.Encode())
		r = http.StatusNotFound
		return
	}

	// Vernon debug
	c.Debugf("Got from user %+v", src)
	c.Debugf("Got from server %+v", dst)

	// Modify attendant
	switch {
	case dst.Attendant + src.Attendant > dst.People:
		c.Warningf("Too many attendants. %d + %d > %d", dst.Attendant, src.Attendant, dst.People)
		r = http.StatusBadRequest
		return
	case dst.Attendant + src.Attendant < 0:
		c.Warningf("Too few attendants. %d%d < 0", dst.Attendant, src.Attendant)
		r = http.StatusBadRequest
		return
	default:
		dst.Attendant += src.Attendant
	}

	// Search whether the member exists
	var i int
	a := dst.Members
	m := src.Members[0]
	for i = 0; i < len(a); i++ {
		if a[i].UserKey == m.UserKey {
			// The member already exists
			break;
		}
	}
	if i == len(a) {
		// Append the new member
		state = stateAppendMember
		a = append(a, m)
		notification.Message += fmt.Sprintf("A new user attended and now item reaches %d/%d. ",
		                                    dst.Attendant,
		                                    dst.People)
		// Vernon debug
		c.Infof("Notify user %s attends item %s and reaches %d/%d", pRequestUser.InstanceId, dst.GcmGroupName, dst.Attendant, dst.People)
	} else {
		// Add attendant to the existing member
		state = stateAddAttendant
		a[i].Attendant += m.Attendant
		notification.Message += fmt.Sprintf("Member attended %d more and now item reaches %d/%d. ",
		                                    m.Attendant,
		                                    dst.Attendant,
		                                    dst.People)
		// Vernon debug
		c.Infof("Existing member %s attends %d more in item %s and reaches %d/%d", pRequestUser.InstanceId, m.Attendant, dst.GcmGroupName, dst.Attendant, dst.People)
	}
	if a[i].Attendant == 0 {
		// The member leaves
		if i == 0 {
			// Delete item because its owner leaves
			state = stateDeleteItem
			notification.Message += "Item is closed because its owner left. "
			// Vernon debug
			c.Infof("Item %s is closed because its owner %s leaves", dst.GcmGroupName, pRequestUser.InstanceId)
		} else {
			// Delete the member from the item
			state = stateDeleteMember
			a[i] = a[len(a)-1]
			a[len(a)-1] = ItemMember{UserKey:"", Attendant:0}
			a = a[:len(a)-1]
			notification.Message += fmt.Sprintf("A member left and item is now %d/%d. ",
		                                        dst.Attendant,
		                                        dst.People)
			// Vernon debug
			c.Infof("User %s leaves item %s. Now %d/%d", pRequestUser.InstanceId, dst.GcmGroupName, dst.Attendant, dst.People)
		}
	} else if i == 0 {
		// Only the owner can update other properties
		// Flag indicates whether item properties are modified
		var flagModified bool = false
		if (src.Image != "") {
			dst.Image = src.Image
			flagModified = true
		}
		if (src.People != 0) {
			dst.People = src.People
			flagModified = true
		}
		if flagModified == true {
			// Set now as the creation time. Precision to a second.
			dst.CreateTime = time.Unix(time.Now().Unix(), 0)
			// Don't update Latitude and Longitude because owner can update anywhere away from the shop
			notification.Message += "Item information updated. "
			// Vernon debug
			c.Infof("Item %s information updated. ", dst.GcmGroupName)
		}
	}

	// Check whether item is finished
	if dst.Attendant == dst.People {
		notification.Message += "Item is finished. Please get together! "
		// Vernon debug
		c.Infof("Item %s is finished. ", dst.GcmGroupName)
	}

	// Update Google Cloud Messaging group
	if gcmResponseCode = updateItemGcmGroup(c, state, &dst, pRequestUser); gcmResponseCode != http.StatusOK {
		r = gcmResponseCode
		return
	}

	// Modify item in datastore
	if state == stateDeleteItem {
		// Vernon debug
		c.Debugf("Item %s is going to be deleted from datastore", key.Encode())

		// Delete item from datastore
		if err = datastore.Delete(c, key); err != nil {
			c.Errorf("%s, in deleting entity by key", err)
			r = http.StatusInternalServerError
			return
		}
		c.Infof("Item %s is deleted from datastore", key.Encode())
	} else {
		// Vernon debug
		c.Debugf("Item %s is going to be modified in datastore", key.Encode())

		// Update item in datastore
		_, err = datastore.Put(c, key, &dst)
		if err != nil {
			c.Errorf("%s in storing in datastore with key %s", err, key.Encode())
			r = http.StatusInternalServerError
			return
		}
		c.Infof("Datastore item is updated %v", dst)
	}

	// Notify all members
	notification.ItemId = key.Encode()
	notification.RequestUserId = pRequestUserKey.Encode()
	if gcmResponseCode = sendItemGcmMessage(c, &dst, &notification); gcmResponseCode != http.StatusOK {
		c.Warningf("Send notification to all members failed")
		r = gcmResponseCode
		return
	}
	return
}

// Send a Google Cloud Messaging message to all the members in the item
// Success: return 200 OK
// Failure: return 500 Internal Server Error
func sendItemGcmMessage(c appengine.Context, pItem *Item, pNotification *ItemUpdateNotification) (r int) {
	// Initial variables
	r = http.StatusOK

	// Make GCM message body
	var bodyString string = fmt.Sprintf(`
		{
			"to":"%s",
			"data": {
				"message":"%s",
				"ItemId":"%s",
				"RequestUserId":"%s"
			}
		}`, pItem.GcmGroupKey, pNotification.Message, pNotification.ItemId, pNotification.RequestUserId)

	// Make a POST request for GCM
	pReq, err := http.NewRequest("POST", GcmURL, strings.NewReader(bodyString))
	if err != nil {
		c.Errorf("%s in makeing a HTTP request", err)
		r = http.StatusInternalServerError
		return
	}
	pReq.Header.Add("Content-Type", "application/json")
	pReq.Header.Add("Authorization", "key="+GcmApiKey)
	// Debug
	c.Infof("Send request to GCM server %s", *pReq)

	// Send request
	var client = urlfetch.Client(c)
	resp, err := client.Do(pReq)
	if err != nil {
		c.Errorf("%s in sending request", err)
		r = http.StatusInternalServerError
		return
	}
	defer resp.Body.Close()

	// Check response
	c.Infof("%d %s", resp.StatusCode, resp.Status)

	// Get response body
	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		c.Errorf("%s in reading response body", err)
		r = http.StatusInternalServerError
		return
	}
	c.Infof("Body: %s", respBody)
	return
}

// Modify the item's Google Cloud Messaging group
// Success: 200 OK
// Failure: 400 Bad Request, 403 Forbidden, 500 Internal Server Error
func updateItemGcmGroup(c appengine.Context, state UpdateItemState, pItem *Item, pUser *User) (r int) {
	// The operation structure which will be sent to GCM server
	var operation GroupOperation
	// The response code received from GCM server
	var gcmResponseCode  int
	// Error
	var err error

	// Initial variables
	r = http.StatusOK

	switch state {
	case stateAppendMember:
		// Vernon debug
		c.Infof("Adding user %s into GCM group %s", pUser.InstanceId, pItem.GcmGroupName)
		// Add the member to the GCM group
		operation.Operation = "add"
		operation.Notification_key_name = pItem.GcmGroupName
		operation.Notification_key = pItem.GcmGroupKey
		operation.Registration_ids = []string{pUser.RegistrationToken}
	 case stateAddAttendant:
		// Do nothing
		return
	case stateDeleteMember:
		// Vernon debug
		c.Infof("Removing user %s from GCM group %s", pUser.InstanceId, pItem.GcmGroupName)
		// Remove the user from the GCM group
		operation.Operation = "remove"
		operation.Notification_key_name = pItem.GcmGroupName
		operation.Notification_key = pItem.GcmGroupKey
		operation.Registration_ids = []string{pUser.RegistrationToken}
	case stateDeleteItem:
		// Vernon debug
		c.Infof("Deleting GCM group %s", pItem.GcmGroupName)
		// Remove all members from the GCM group so that the group will be removed at the same time
		operation.Operation = "remove"
		operation.Notification_key_name = pItem.GcmGroupName
		operation.Notification_key = pItem.GcmGroupKey
		operation.Registration_ids = make([]string, 0, len(pItem.Members))

		// Set all members' registration token
		for _, v := range pItem.Members {
			var user User
			var pUserKey *datastore.Key

			// Search user registration token
			if pUserKey, err = datastore.DecodeKey(v.UserKey); err != nil {
				// This should never happen
				c.Errorf("%s in decodeing user key %s of existing member in group %s. This should never happen.", err, v.UserKey, pItem.GcmGroupName)
				continue
			}
			if err = datastore.Get(c, pUserKey, &user); err != nil {
				// This should never happen
				c.Errorf("%s in getting user from datastore with key %s. This should never happen.", err, v.UserKey)
				continue
			}

			operation.Registration_ids = append(operation.Registration_ids, user.RegistrationToken)
		}
	}

	// Send the operation to GCM server
	if gcmResponseCode = sendGroupOperationToGcm(&operation, c); gcmResponseCode != http.StatusOK {
		c.Errorf("Send group operation %+v to GCM faied", operation)
		r = gcmResponseCode
		return
	}
	c.Infof("%s user %s to/from GCM group %s", operation.Operation, operation.Registration_ids, operation.Notification_key_name)

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

