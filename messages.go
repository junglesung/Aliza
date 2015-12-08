package aliza

import (
	"appengine"
	"appengine/datastore"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strings"
	"fmt"
	"appengine/urlfetch"
)

// HTTP body of sending a message to a user
type UserMessage struct {
	// To authentication
	InstanceId           string    `json:"instanceid"`
	// To the target user
	UserId               string    `json:"userid"`      // Datastore user kind key string
	Message              string    `json:"message"`
}

// HTTP body of sending a message to a topic
type TopicMessage struct {
	// To authentication
	InstanceId           string    `json:"instanceid"`
	// To the target user
	Topic                string    `json:"topic"`
	Message              string    `json:"message"`
}

// HTTP body of sending a message to a group
type GroupMessage struct {
	// To authentication
	InstanceId           string    `json:"instanceid"`
	// To the target user
	GroupName            string    `json:"groupname"`
	Message              string    `json:"message"`
}

// GCM server
const GcmURL = "https://gcm-http.googleapis.com/gcm/send"

// Receive a message from an APP instance.
// Check it's instancd ID.
// Send the message back.
// POST ./user-messages"
// Success: 204 No Content
// Failure: 400 Bad Request, 403 Forbidden
func SendUserMessage(rw http.ResponseWriter, req *http.Request) {
	// Appengine
	var c appengine.Context = appengine.NewContext(req)
	// Result, 0: success, 1: failed
	var r int = http.StatusNoContent

	// Return code
	defer func() {
		// Return status. WriteHeader() must be called before call to Write
		if r == http.StatusNoContent {
			// Changing the header after a call to WriteHeader (or Write) has no effect.
			rw.WriteHeader(http.StatusNoContent)
		} else if r == http.StatusBadRequest {
			//			http.Error(rw, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			http.Error(rw, `Please follow https://aaa.appspot.com/api/0.1/user-messages
			                {
			                    "instanceid":""
			                    "userid":""
			                    "message":""
			                }`, http.StatusBadRequest)
		} else {
			http.Error(rw, http.StatusText(r), r)
		}
	}()

	// Get body
	b, err := ioutil.ReadAll(req.Body)
	if err != nil {
		c.Errorf("%s in reading body %s", err, b)
		r = http.StatusBadRequest
		return
	}
	var message UserMessage
	if err = json.Unmarshal(b, &message); err != nil {
		c.Errorf("%s in decoding body %s", err, b)
		r = http.StatusBadRequest
		return
	}

	// Authenticate sender
	var isValid bool = false
	isValid, err = verifyRequest(message.InstanceId, c)
	if err != nil {
		c.Errorf("%s in authenticating request", err)
		r = http.StatusBadRequest
		return
	}
	if isValid == false {
		c.Warningf("Invalid request, ignore")
		r = http.StatusForbidden
		return
	}

	// Decode datastore key from string
	key, err := datastore.DecodeKey(message.UserId)
	if err != nil {
		c.Errorf("%s in decoding key string", err)
		r = http.StatusBadRequest
		return
	}

	// Get target user from datastore
	var dst User
	if err := datastore.Get(c, key, &dst); err != nil {
		c.Errorf("%s in getting entity from datastore by key %s", err, message.UserId)
		r = http.StatusNotFound
		return
	}

	// Make GCM message body
	var bodyString string = fmt.Sprintf(`
		{
			"to":"%s",
			"data": {
				"message":"%s"
			}
		}`, dst.RegistrationToken, message.Message)


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
	c.Infof("%s", *pReq)

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
	c.Infof("%s", respBody)
}

// Receive a message from an APP instance.
// Check it's instancd ID.
// Send the message to the topic.
// POST ./topic-messages"
// Success: 204 No Content
// Failure: 400 Bad Request, 403 Forbidden, 404 NotFound, 500 InternalError
func SendTopicMessage(rw http.ResponseWriter, req *http.Request) {
	// Appengine
	var c appengine.Context = appengine.NewContext(req)
	// Result, 0: success, 1: failed
	var r int = http.StatusNoContent

	// Return code
	defer func() {
		// Return status. WriteHeader() must be called before call to Write
		if r == http.StatusNoContent {
			// Changing the header after a call to WriteHeader (or Write) has no effect.
			rw.WriteHeader(http.StatusNoContent)
		} else if r == http.StatusBadRequest {
			//			http.Error(rw, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			http.Error(rw, `Please follow https://aaa.appspot.com/api/0.1/topic-messages
			                {
			                    "instanceid":""
			                    "topic":""
			                    "message":""
			                }`, http.StatusBadRequest)
		} else {
			http.Error(rw, http.StatusText(r), r)
		}
	}()

	// Get body
	b, err := ioutil.ReadAll(req.Body)
	if err != nil {
		c.Errorf("%s in reading body %s", err, b)
		r = http.StatusBadRequest
		return
	}
	var message TopicMessage
	if err = json.Unmarshal(b, &message); err != nil {
		c.Errorf("%s in decoding body %s", err, b)
		r = http.StatusBadRequest
		return
	}

	// Authenticate sender
	var isValid bool = false
	isValid, err = verifyRequest(message.InstanceId, c)
	if err != nil {
		c.Errorf("%s in authenticating request", err)
		r = http.StatusBadRequest
		return
	}
	if isValid == false {
		c.Warningf("Invalid request, ignore")
		r = http.StatusForbidden
		return
	}

	// Make GCM message body
	var bodyString string = fmt.Sprintf(`
		{
			"to":"/topics/%s",
			"data": {
				"message":"%s"
			}
		}`, message.Topic, message.Message)


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
	c.Infof("%s", *pReq)

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
	c.Infof("%s", respBody)
}

// Receive a message from an APP instance.
// Check it's instancd ID.
// Send the message to the gruop.
// POST ./group-messages"
// Success: 204 No Content
// Failure: 400 Bad Request, 403 Forbidden, 404 NotFound, 500 InternalError
func SendGroupMessage(rw http.ResponseWriter, req *http.Request) {
	// Appengine
	var c appengine.Context = appengine.NewContext(req)
	// Result, 0: success, 1: failed
	var r int = http.StatusNoContent

	// Return code
	defer func() {
		// Return status. WriteHeader() must be called before call to Write
		if r == http.StatusNoContent {
			// Changing the header after a call to WriteHeader (or Write) has no effect.
			rw.WriteHeader(http.StatusNoContent)
		} else if r == http.StatusBadRequest {
			//			http.Error(rw, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			http.Error(rw, `Please follow https://aaa.appspot.com/api/0.1/group-messages
			                {
			                    "instanceid":""
			                    "groupName":""
			                    "message":""
			                }`, http.StatusBadRequest)
		} else {
			http.Error(rw, http.StatusText(r), r)
		}
	}()

	// Get body
	b, err := ioutil.ReadAll(req.Body)
	if err != nil {
		c.Errorf("%s in reading body %s", err, b)
		r = http.StatusBadRequest
		return
	}
	var message GroupMessage
	if err = json.Unmarshal(b, &message); err != nil {
		c.Errorf("%s in decoding body %s", err, b)
		r = http.StatusBadRequest
		return
	}

	// Authenticate sender
	var isValid bool = false
	isValid, err = verifyRequest(message.InstanceId, c)
	if err != nil {
		c.Errorf("%s in authenticating request", err)
		r = http.StatusBadRequest
		return
	}
	if isValid == false {
		c.Warningf("Invalid request, ignore")
		r = http.StatusForbidden
		return
	}

	// Search for existing group
	var cKey *datastore.Key
	var pGroup *Group
	cKey, pGroup, err = searchGroup(message.GroupName, c)
	if err != nil {
		c.Errorf("%s in searching existing group %s", err, message.GroupName)
		r = http.StatusInternalServerError
		return
	}
	if cKey == nil {
		c.Warningf("Group %s is not found", message.GroupName)
		r = http.StatusBadRequest
		return
	}

	// Make GCM message body
	var bodyString string = fmt.Sprintf(`
		{
			"to":"%s",
			"data": {
				"message":"%s"
			}
		}`, pGroup.NotificationKey, message.Message)


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
	c.Infof("%s", respBody)
}
