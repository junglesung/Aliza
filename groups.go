package aliza

import (
	"appengine"
	"appengine/datastore"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strings"
	"appengine/urlfetch"
	"errors"
	"bytes"
)

// Data structure got from datastore group kind
type Group struct {
	Name                 string    `json:"name"`
	Owner                string    `json:"owner"`       // Instance ID
	Members            []string    `json:"members"`     // Instance ID list
	NotificationKey      string    `json:"notificationkey"`  // GCM device group unique ID
}

// HTTP body of joining or leaving group requests from users
type GroupUser struct {
	// To authentication
	InstanceId           string    `json:"instanceid"`
	// The group
	GroupName            string    `json:"groupname"`
}

// HTTP body to send to Google Cloud Messaging server to manage device groups
type GroupOperation struct {
	Operation            string    `json:"operation"`              // "create", "add", "remove"
	Notification_key_name string   `json:"notification_key_name"`  // A unique group name in a Google project
	Notification_key     string    `json:"notification_key,omitempty"`       // A unique key to identify a group
	Registration_ids   []string    `json:"registration_ids"`       // APP registration tokens in the group
}

// HTTP body received from Google Cloud Messaging server
type GroupOperationResponse struct {
	Notification_key     string    `json:"notification_key"`       // A unique key to identify a group
	Error                string    `json:"error"`                  // Error message
}

const GroupKind = "Group"
const GroupRoot = "Group root"

// GCM server
const GcmGroupURL = "https://android.googleapis.com/gcm/notification"

// PUT ./groups"
// Success: 204 No Content
// Failure: 400 Bad Request, 403 Forbidden, 500 Internal Server Error
func JoinGroup(rw http.ResponseWriter, req *http.Request) {
	// Appengine
	var c appengine.Context = appengine.NewContext(req)
	// Return code in a HTTP response
	var r int = http.StatusNoContent
	var err error = nil

	// Write response finally
	defer func() {
		if r == http.StatusNoContent {
			// Changing the header after a call to WriteHeader (or Write) has no effect.
			// ... Ex, rw.Header().Set("Location", req.URL.String() + "/" + cKey.Encode())
			// Return status. WriteHeader() must be called before call to Write
			rw.WriteHeader(r)
		} else {
			http.Error(rw, http.StatusText(r), r)
		}
	}()

	// Get data from body
	b, err := ioutil.ReadAll(req.Body)
	if err != nil {
		c.Errorf("%s in reading body %s", err, b)
		r = http.StatusInternalServerError
		return
	}

	// Vernon debug
	c.Debugf("Got body %s", b)

	var user GroupUser
	if err = json.Unmarshal(b, &user); err != nil {
		c.Errorf("%s in decoding body %s", err, b)
		r = http.StatusBadRequest
		return
	}

	r = joinGroup(c, user)
}

func joinGroup(c appengine.Context, user GroupUser) (r int) {
	var cKey *datastore.Key = nil
	var err error = nil

	// Initial variables
	r = http.StatusNoContent

	// Authenticate sender & Search for user registration token
	var pUser *User
	var token string
	_, pUser, err = searchUser(user.InstanceId, c)
	if err != nil {
		c.Errorf("%s in searching user %v", err, user.InstanceId)
		r = http.StatusInternalServerError
		return
	}
	if pUser == nil {
		c.Errorf("User %s not found. Invalid request. Ignore.", user.InstanceId)
		r = http.StatusForbidden
		return
	}
	token = pUser.RegistrationToken

	// Search for existing group
	var pGroup *Group
	cKey, pGroup, err = searchGroup(user.GroupName, c)
	if err != nil {
		c.Errorf("%s in searching existing group %s", err, user.GroupName)
		r = http.StatusInternalServerError
		return
	}

	// Make GCM message body
	var operation GroupOperation
	if cKey == nil {
		// Create a new group on GCM server
		operation.Operation = "create"
		operation.Notification_key_name = user.GroupName
		operation.Registration_ids = []string{token}
		if r = sendGroupOperationToGcm(&operation, c); r != http.StatusOK {
			c.Errorf("Send group operation to GCM failed")
			return
		}
		r = http.StatusNoContent

		// Add new group to the datastore
		pGroup = &Group {
			Name: user.GroupName,
			Owner: user.InstanceId,
			Members: []string {user.InstanceId},
			NotificationKey: operation.Notification_key,
		}
		var pKey *datastore.Key = datastore.NewKey(c, GroupKind, GroupRoot, 0, nil)
		cKey, err = datastore.Put(c, datastore.NewIncompleteKey(c, GroupKind, pKey), pGroup)
		if err != nil {
			c.Errorf("%s in storing to datastore", err)
			r = http.StatusInternalServerError
			return
		}
		c.Infof("Create group %+v", pGroup)
	} else {
		// Add the new user to the existing group on GCM server
		operation.Operation = "add"
		operation.Notification_key_name = user.GroupName
		operation.Notification_key = pGroup.NotificationKey
		operation.Registration_ids = []string{token}
		if r = sendGroupOperationToGcm(&operation, c); r != http.StatusOK {
			c.Errorf("Send group operation to GCM failed")
			return
		}
		r = http.StatusNoContent

		// Modify datastore
		pGroup.Members = append(pGroup.Members, token)
		cKey, err = datastore.Put(c, cKey, pGroup)
		if err != nil {
			c.Errorf("%s in storing to datastore", err)
			r = http.StatusInternalServerError
			return
		}
		c.Infof("Add user %s to group %s", user.InstanceId, user.GroupName)
	}
	return
}

// DELETE ./groups/xxx", xxx: Group name
// Header {"Instance-Id":"..."}
// Success returns 204 No Content
// Failure returns 400 Bad Request, 403 Forbidden, 500 Internal Server Error
func LeaveGroup(rw http.ResponseWriter, req *http.Request) {
	// Appengine
	var c appengine.Context = appengine.NewContext(req)
	// Result, 0: success, 1: failed
	var r int = http.StatusNoContent
	// Sender instance ID
	var instanceId string
	// Group name to leave
	var groupName string
	// Function to write response header
	defer func() {
		if r == http.StatusNoContent {
			// Return status. WriteHeader() must be called before call to Write
			rw.WriteHeader(r)
		} else {
			http.Error(rw, http.StatusText(r), r)
		}
	}()

	// Get instance ID from header
	instanceId = req.Header.Get("Instance-Id")
	if instanceId == "" {
		c.Warningf("Missing instance ID. Ignore the request.")
		r = http.StatusBadRequest
		return
	}

	// Get group name from URL
	var tokens []string
	tokens = strings.Split(req.URL.Path, "/")
	for i, v := range tokens {
		if v == "groups" && i + 1 < len(tokens) {
			groupName = tokens[i + 1]
			break
		}
	}
	if groupName == "" {
		c.Warningf("Missing group name. Ignore the request.")
		r = http.StatusBadRequest
		return
	}

	// Vernon debug
	c.Debugf("User %s is going to leave group %s", instanceId, groupName)

	r = leaveGroup(c, instanceId, groupName)
}

func leaveGroup(c appengine.Context, instanceId string, groupName string) (r int) {
	// Sender registration token
	var registrationToken string
	// Then operation sent to GCM server
	var operation GroupOperation
	// Group in datastore
	var cKey *datastore.Key
	var pGroup *Group
	// Error
	var err error

	// Initial variables
	r = http.StatusNoContent

	// Authenticate sender & Search for user registration token
	var pUser *User
	_, pUser, err = searchUser(instanceId, c)
	if err != nil {
		c.Errorf("%s in searching user %v", err, instanceId)
		r = http.StatusInternalServerError
		return
	}
	if pUser == nil {
		c.Errorf("User %s not found. Invalid request. Ignore.", instanceId)
		r = http.StatusForbidden
		return
	}
	registrationToken = pUser.RegistrationToken

	// Search for existing group
	cKey, pGroup, err = searchGroup(groupName, c)
	if err != nil {
		c.Errorf("%s in searching existing group %s", err, groupName)
		r = http.StatusInternalServerError
		return
	}
	if cKey == nil {
		c.Infof("Group %s has been deleted already", groupName)
		return
	}

	var returnCode int = http.StatusOK
	if instanceId == pGroup.Owner {
		// Vernon debug
		c.Debugf("User %s owns the group %s", instanceId, groupName)

		// Remove all user from GCM server so that the group will be removed at the same time
		for _, v := range pGroup.Members {
			// Search user registration token
			_, pUser, err = searchUser(v, c)
			if err != nil {
				c.Warningf("%s in searching user %v", err, v)
				continue
			}
			if pUser == nil {
				c.Warningf("User %s not found. Ignore.", v)
				continue
			}
			registrationToken = pUser.RegistrationToken

			// Make operation structure
			operation.Operation = "remove"
			operation.Notification_key_name = pGroup.Name
			operation.Notification_key = pGroup.NotificationKey
			operation.Registration_ids = []string{registrationToken}
			if returnCode = sendGroupOperationToGcm(&operation, c); returnCode != http.StatusOK {
				c.Warningf("Failed to remove user %s from group %s because sending group operation to GCM failed", v, groupName)
				r = returnCode
				continue
			}
			c.Infof("User %s is removed from group %s", pUser.InstanceId, groupName)
		}

		// Modify datastore
		if err = datastore.Delete(c, cKey); err != nil {
			c.Errorf("%s in delete group %s from datastore", err, groupName)
			r = http.StatusInternalServerError
			return
		}
		c.Infof("User %s removed group %s", instanceId, groupName)
	} else {
		// Vernon debug
		c.Debugf("User %s doesn't own the group %s", instanceId, groupName)

		// Remove the user from the existing group on GCM server
		operation.Operation = "remove"
		operation.Notification_key_name = groupName
		operation.Notification_key = pGroup.NotificationKey
		operation.Registration_ids = []string{registrationToken}
		if returnCode = sendGroupOperationToGcm(&operation, c); returnCode != http.StatusOK {
			c.Errorf("Send group operation to GCM failed")
			r = returnCode
			return
		}

		// Modify datastore
		a := pGroup.Members
		for i, x := range a {
			if x == instanceId {
				a[i] = a[len(a)-1]
				a[len(a)-1] = ""
				a = a[:len(a)-1]
				break
			}
		}
		pGroup.Members = a
		cKey, err = datastore.Put(c, cKey, pGroup)
		if err != nil {
			c.Errorf("%s in storing to datastore", err)
			r = http.StatusInternalServerError
			return
		}
		c.Infof("Remove user %s from group %s", instanceId, groupName)
	}

	return
}

// Send a Google Cloud Messaging Device Group operation to GCM server
// Success: 200 OK. Store the notification key from server to the operation structure
// Failure: 400 Bad Request, 403 Forbidden, 500 Internal Server Error
func sendGroupOperationToGcm(pOperation *GroupOperation, c appengine.Context) (r int) {
	// Initial variables
	var err error = nil
	r = http.StatusOK

	// Check parameters
	if pOperation == nil {
		c.Errorf("Parameter pOperation is nil")
		r = http.StatusInternalServerError
		return
	}

	// Vernon debug
	c.Debugf("GCM operation %+v", pOperation)

	// Make a POST request for GCM
	var b []byte
	b, err = json.Marshal(pOperation)
	if err != nil {
		c.Errorf("%s in encoding an operation as JSON", err)
		r = http.StatusBadRequest
		return
	}
	pReq, err := http.NewRequest("POST", GcmGroupURL, bytes.NewReader(b))
	if err != nil {
		c.Errorf("%s in makeing a HTTP request", err)
		r = http.StatusInternalServerError
		return
	}
	pReq.Header.Add("Content-Type", "application/json")
	pReq.Header.Add("Authorization", "key="+GcmApiKey)
	pReq.Header.Add("project_id", GaeProjectNumber)
	// Debug
	c.Debugf("Send request to GCM server %s", *pReq)
	c.Debugf("Send body to GCM server %s", b)

	// Send request
	var client = urlfetch.Client(c)
	resp, err := client.Do(pReq)
	if err != nil {
		c.Errorf("%s in sending request", err)
		r = http.StatusInternalServerError
		return
	}

	// Get response body
	var respBody GroupOperationResponse
	defer resp.Body.Close()
	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		c.Errorf("%s in reading response body", err)
		r = http.StatusInternalServerError
		return
	}
	c.Infof("%s", b)
	if err = json.Unmarshal(b, &respBody); err != nil {
		c.Errorf("%s in decoding JSON response body", err)
		r = http.StatusInternalServerError
		return
	}

	// Check response
	c.Infof("%d %s", resp.StatusCode, resp.Status)
	if resp.StatusCode == http.StatusOK {
		// Success. Write Notification Key to operation structure
		pOperation.Notification_key = respBody.Notification_key
		return
	} else {
		c.Errorf("GCM server replied that %s", respBody.Error)
		r = http.StatusBadRequest
		return
	}
}

func searchGroup(name string, c appengine.Context) (key *datastore.Key, group *Group, err error) {
	var v []Group
	// Initial variables
	key = nil
	group = nil
	err = nil

	// Query
	f := datastore.NewQuery(GroupKind)
	f = f.Filter("Name=", name)
	k, err := f.GetAll(c, &v)
	if err != nil {
		c.Errorf("%s in getting data from datastore\n", err)
		err = errors.New("Datastore is temporary unavailable")
		return
	}

	if k == nil || len(k) == 0 {
		return
	}

	key = k[0]
	group = &v[0]
	return
}
