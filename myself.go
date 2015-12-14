package aliza

import (
	"appengine"
	"appengine/datastore"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"time"
	"appengine/urlfetch"
	"errors"
)

// Data structure got from datastore user kind
type User struct {
	InstanceId           string    `json:"instanceid"`
	RegistrationToken    string    `json:"registrationtoken"`
	LastUpdateTime       time.Time `json:"lastupdatetime"`
}

// HTTP response body from Google Instance ID authenticity service
type UserRegistrationTokenAuthenticity struct {
	Application string             `json:"application"`
	AuthorizedEntity string        `json:"authorizedEntity"`
	// Other properties in the response body are "don't care"
}

// HTTP response body to user registration
type UserRegistrationResponseBody struct {
	UserId string                  `json:"userid"`
}

const UserKind = "User"
const UserRoot = "User root"
const AppNamespace = "com.vernonsung.testquerygcs"
const GaeProjectNumber = "492596673998"

// GCM server
const InstanceIdVerificationUrl = "https://iid.googleapis.com/iid/info/"
const GcmApiKey = "AIzaSyBgH4A9CdPgc-Kh54j5TLgKl7x3YCGBtOU"

// HTTP header
const HttpHeaderInstanceId = "Instance-Id"

// PUT ./myself"
// Success: 200 OK
// Failure: 400 Bad Request
func UpdateMyself(rw http.ResponseWriter, req *http.Request) {
	// Appengine
	var c appengine.Context = appengine.NewContext(req)
	// Result, 0: success, 1: failed
	var r int = 0
	var cKey *datastore.Key = nil
	defer func() {
		// Return status. WriteHeader() must be called before call to Write
		if r == 0 {
			// Return status. WriteHeader() must be called before call to Write
			rw.WriteHeader(http.StatusOK)
			// Return body
			var dst UserRegistrationResponseBody = UserRegistrationResponseBody{ UserId:cKey.Encode() }
			if err := json.NewEncoder(rw).Encode(dst); err != nil {
				c.Errorf("%s in encoding result %v", err, dst)
			}
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

	// Vernon debug
	c.Debugf("Got body %s", b)

	var user User
	if err = json.Unmarshal(b, &user); err != nil {
		c.Errorf("%s in decoding body %s", err, b)
		r = 1
		return
	}

	// Check registration token starts with instance ID. That's the rule of Google API service authenticity
	// Also check registration token is official-signed by sending the token to Google token authenticity check service
	if user.RegistrationToken[0:len(user.InstanceId)] != user.InstanceId || isRegistrationTokenValid(user.RegistrationToken, c) == false {
		c.Errorf("Instance ID %s is invalid", user.InstanceId)
		r = 1
		return
	}

	// Set now as the creation time. Precision to a second.
	user.LastUpdateTime = time.Unix(time.Now().Unix(), 0)

	// Search for existing user
	var pKey *datastore.Key
	var pOldUser *User
	pKey, pOldUser, err = searchUser(user.InstanceId, c)
	if err != nil {
		c.Errorf("%s in searching existing user %v", err, user)
		r = 1
		return
	}
	if pKey == nil {
		// Add new user into datastore
		pKey = datastore.NewKey(c, UserKind, UserRoot, 0, nil)
		cKey, err = datastore.Put(c, datastore.NewIncompleteKey(c, UserKind, pKey), &user)
		if err != nil {
			c.Errorf("%s in storing to datastore", err)
			r = 1
			return
		}
		c.Infof("Add user %+v", user)
	} else if user.RegistrationToken == pOldUser.RegistrationToken {
		// Duplicate request. Do nothing to datastore and return existing key
		cKey = pKey
	} else {
		cKey, err = datastore.Put(c, pKey, &user)
		if err != nil {
			c.Errorf("%s in storing to datastore", err)
			r = 1
			return
		}
		c.Infof("Update user %+v", user)
	}
}

// Send APP instance ID to Google server to verify its authenticity
func isRegistrationTokenValid(token string, c appengine.Context) (isValid bool) {
	if token == "" {
		c.Warningf("Instance ID is empty")
		return false
	}

	// Make a GET request for Google Instance ID service
	pReq, err := http.NewRequest("GET", InstanceIdVerificationUrl + token, nil)
	if err != nil {
		c.Errorf("%s in makeing a HTTP request", err)
		return false
	}
	pReq.Header.Add("Authorization", "key="+GcmApiKey)
	// Debug
	c.Infof("%s", *pReq)

	// Send request
	pClient := urlfetch.Client(c)
	var resp *http.Response
	var sleepTime int
	// A Google APP Engine process must end within 60 seconds. So sleep no more than 16 seconds each retry.
	for sleepTime = 1; sleepTime <= 16; sleepTime *= 2 {
		resp, err = pClient.Do(pReq)
		if err != nil {
			c.Errorf("%s in verifying instance ID %s", err, token)
			return false
		}
		// Retry while server is temporary invalid
		if resp.StatusCode != http.StatusServiceUnavailable {
			break
		}
		time.Sleep(1 * time.Second)
	}

	// Check response code
	if resp.StatusCode != http.StatusOK {
		c.Warningf("Invalid instance ID with response code %d %s", resp.StatusCode, resp.Status)
		return false
	}

	// Get body
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		c.Errorf("%s in reading HTTP response body")
		return false
	}

	// Decode body as JSON
	var authenticity UserRegistrationTokenAuthenticity
	if err := json.Unmarshal(body, &authenticity); err != nil {
		c.Warningf("%s in decoding HTTP response body %s", body)
		return false
	}
	if authenticity.Application != AppNamespace || authenticity.AuthorizedEntity != GaeProjectNumber {
		c.Warningf("Invalid instance ID with authenticity application %s and authorized entity %s",
			authenticity.Application, authenticity.AuthorizedEntity)
		return false
	}

	return true
}

func searchUser(instanceId string, c appengine.Context) (key *datastore.Key, user *User, err error) {
	var v []User
	// Initial variables
	key = nil
	user = nil
	err = nil

	// Query
	f := datastore.NewQuery(UserKind)
	f = f.Filter("InstanceId=", instanceId)
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
	user = &v[0]
	return
}

func verifyRequest(instanceId string, c appengine.Context) (isValid bool, err error) {
	// Search for user from datastore
	var pUser *User

	// Initial variables
	isValid = false
	err = nil

	_, pUser, err = searchUser(instanceId, c)
	if err != nil {
		c.Errorf("%s in searching user %v", err, instanceId)
		return
	}

	// Verify registration token
	if pUser == nil {
		c.Warningf("Invalid instance ID %s is not found in datastore. Ignore the request", instanceId)
		return
	}
	isValid = true
	return
}

func VerifyRequest(rw http.ResponseWriter, req *http.Request) (isvalid int) {
	var instanceId string = req.Header.Get(HttpHeaderInstanceId)
	var c appengine.Context = appengine.NewContext(req)
	var isValid bool = false
	isValid, _ = verifyRequest(instanceId, c);
	return isValid
}