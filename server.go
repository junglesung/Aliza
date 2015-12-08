package aliza

import (
	"appengine"
	"net/http"
)

const BaseUrl = "/api/0.1/"

func init() {
	http.HandleFunc(BaseUrl, rootPage)
	http.HandleFunc(BaseUrl+"queryAll", queryAllItem)
	http.HandleFunc(BaseUrl+"storeImage", storeImage)
	http.HandleFunc(BaseUrl+"deleteAll", deleteAllItem)
	http.HandleFunc(BaseUrl+"images", images)
	http.HandleFunc(BaseUrl+"items", items)
	http.HandleFunc(BaseUrl+"items/", items)
	http.HandleFunc(BaseUrl+"myself", UpdateMyself)  // PUT
	http.HandleFunc(BaseUrl+"groups", groups)  // PUT
	http.HandleFunc(BaseUrl+"groups/", groups)  // DELETE
	http.HandleFunc(BaseUrl+"user-messages", SendUserMessage)  // POST
	http.HandleFunc(BaseUrl+"topic-messages", SendTopicMessage)  // POST
	http.HandleFunc(BaseUrl+"group-messages", SendGroupMessage)  // POST
}

func rootPage(rw http.ResponseWriter, req *http.Request) {
	c := appengine.NewContext(req)
	c.Debugf("This is root")
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
	case "PUT":
		updateItem(rw, req)
	case "DELETE":
		deleteItem(rw, req)
	default:
		queryAllItem(rw, req)
	}
}

func groups(rw http.ResponseWriter, req *http.Request) {
	switch req.Method {
	//	case "GET":
	//		listMember(rw, req)
	//	case "POST":
	//		SendMessage(rw, req)
	case "PUT":
		JoinGroup(rw, req)
	case "DELETE":
		LeaveGroup(rw, req)
	default:
		http.Error(rw, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
	}
}
