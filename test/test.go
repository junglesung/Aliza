package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Item struct {
	Id         string    `json:"id"          datastore:"-"`
	Image      string    `json:"image"`
	People     int       `json:"people"`
	Attendant  int       `json:"attendant"`
	Latitude   float64   `json:"latitude"`
	Longitude  float64   `json:"longitude"`
	CreateTime time.Time `json:"createtime"`
}

// Target server
const ItemURL = "https://testgcsserver.appspot.com/api/0.1/"
// const ItemURL = "http://127.0.0.1:8080/api/0.1/"

const ItemMinPeople = 2
const ItemMaxPeople = 5

// Pring an Item
func (b Item) String() string {
	s := ""
	s += fmt.Sprintln("Image:", b.Image)
	s += fmt.Sprintln("People:", b.People)
	s += fmt.Sprintln("Attendant:", b.Attendant)
	s += fmt.Sprintln("Latitude:", b.Latitude)
	s += fmt.Sprintln("Longitude:", b.Longitude)
	s += fmt.Sprintln("CreateTime:", b.CreateTime)
	return s
}

func queryAll() {
	// Send request
	resp, err := http.Get(ItemURL + "items")
	if err != nil {
		fmt.Println(err)
		return
	}

	// Print status
	fmt.Println(resp.Status, resp.StatusCode)

	// Get body
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Decode body
	var items []Item
	// var items map[string]Item = make(map[string]Item)
	if resp.StatusCode == http.StatusOK {
		// Decode as JSON
		if err := json.Unmarshal(body, &items); err != nil {
			fmt.Println(err, "in decoding JSON")
			return
		}
		for _, v := range items {
			fmt.Println("-------------------------------")
			fmt.Println(v)
		}
		fmt.Println("Total", len(items), "items")
	} else {
		// Decode as text
		fmt.Printf("%s", body)
	}
}

func searchItem() {
	// Make URL
	var u *url.URL
	var err error
	if u, err = url.ParseRequestURI(ItemURL + "items"); err != nil {
		fmt.Println(err, "in making URL")
		return
	}

	// Set data filter
	var q url.Values = u.Query()
	var people int = rand.Intn(ItemMaxPeople - ItemMinPeople) + ItemMinPeople
	q.Add("People", strconv.Itoa(people))
	u.RawQuery = q.Encode()
	fmt.Println("Query people =", people)

	// Send request
	resp, err := http.Get(u.String())
	if err != nil {
		fmt.Println(err)
		return
	}

	// Print status
	fmt.Println(resp.Status, resp.StatusCode)

	// Get body
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Decode body
	var items []Item
	// var items map[string]Item = make(map[string]Item)
	if resp.StatusCode == http.StatusOK {
		// Decode as JSON
		if err := json.Unmarshal(body, &items); err != nil {
			fmt.Println(err, "in decoding JSON")
			return
		}
		for _, v := range items {
			fmt.Println("-------------------------------")
			fmt.Println(v)
		}
		fmt.Println("Total", len(items), "items")
	} else {
		// Decode as text
		fmt.Printf("%s", body)
	}
}

// Return
// int = 0: success
//       1: failed
// string is the new item's unique key
func storeItem(imgUrl string) (r int, key string) {
	// Return value
	r = 0
	key = ""

	// Make body
	item := Item{
		Id:         "",
		Image:      imgUrl,
		People:     (rand.Intn(ItemMaxPeople - ItemMinPeople) + ItemMinPeople),
		Attendant:  1,
		Latitude:   25.034238,  // Taipei 101
		Longitude:  121.564515,  // Taipei 101
		CreateTime: time.Now(),
	}
	b, err := json.Marshal(item)
	if err != nil {
		fmt.Println(err, "in encoding a item as JSON")
		r = 1
		return
	}

	// Send request
	resp, err := http.Post(ItemURL+"items", "application/json", bytes.NewReader(b))
	if err != nil {
		fmt.Println(err)
		r = 1
		return
	}
	defer resp.Body.Close()
	fmt.Println(resp.Status, resp.StatusCode)
	if resp.StatusCode != http.StatusCreated {
		r = 1
		return
	}
	url, err := resp.Location()
	if err != nil {
		fmt.Println(err, "in getting location from response")
		return
	}
	fmt.Println("Location is", url)

	// Get key from URL
	tokens := strings.Split(url.Path, "/")
	var keyIndexInTokens int = 0
	for i, v := range tokens {
		if v == "items" {
			keyIndexInTokens = i + 1
		}
	}
	if keyIndexInTokens >= len(tokens) {
		fmt.Println("Key is not given")
		return
	}
	key = tokens[keyIndexInTokens]
	if key == "" {
		fmt.Println("Key is empty")
		return
	}
	return
}

// Return 0: success
// Return 1: failed
func queryItem(key string) int {
	// Vernon debug
	fmt.Println(ItemURL+"items/"+key)

	// Send request
	resp, err := http.Get(ItemURL+"items/"+key)
	if err != nil {
		fmt.Println(err)
		return 1
	}

	// Print status
	fmt.Println(resp.Status, resp.StatusCode)

	// Get body
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return 1
	}

	// Decode body
	var item Item
	if resp.StatusCode == http.StatusOK {
		// Decode as JSON
		if err := json.Unmarshal(body, &item); err != nil {
			fmt.Println(err, "in decoding JSON", body)
			return 1
		}
		fmt.Println(item)
		// Vernon debug
		fmt.Printf("%s\n", body)
	} else {
		// Decode as text
		fmt.Printf("%s\n", body)
		return 1
	}

	return 0
}

// Return 0: success
// Return 1: failed
func updateItem(key string) int {
	// Vernon debug
	fmt.Println(ItemURL+"items/"+key)

	// Make body
	item := Item{
		Id:         "",
		Image:      "",
		People:     0,
		Attendant:  1,
		Latitude:   0,
		Longitude:  0,
		CreateTime: time.Now(),
	}
	b, err := json.Marshal(item)
	if err != nil {
		fmt.Println(err, "in encoding a item as JSON")
		return 1
	}

	// Make a PUT request
	pReq, err := http.NewRequest("PUT", ItemURL+"items/"+key, bytes.NewReader(b))
	if err != nil {
		fmt.Println(err)
		return 1
	}
	pReq.Header.Add("Content-Type", "application/json")

	// Send request
	var client = &http.Client{}
	resp, err := client.Do(pReq)
	if err != nil {
		fmt.Println(err)
		return 1
	}
	defer resp.Body.Close()

	// Check response
	fmt.Println(resp.Status, resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		return 1
	}

	return 0
}

// Return 0: success
// Return 1: failed
func deleteItem(key string) int {
	// Send request
	pReq, err := http.NewRequest("DELETE", ItemURL+"items/"+key, nil)
	if err != nil {
		fmt.Println(err, "in making request")
		return 1
	}
	resp, err := http.DefaultClient.Do(pReq)
	if err != nil {
		fmt.Println(err, "in sending request")
		return 1
	}
	defer resp.Body.Close()
	fmt.Println(resp.Status, resp.StatusCode)
	if resp.StatusCode == http.StatusNoContent {
		return 0
	} else {
		return 1
	}
}

// Return 0: success
// Return 1: failed
func deleteAll() int {
	// Send request
	pReq, err := http.NewRequest("DELETE", ItemURL+"items", nil)
	if err != nil {
		fmt.Println(err, "in making request")
		return 1
	}
	resp, err := http.DefaultClient.Do(pReq)
	if err != nil {
		fmt.Println(err, "in sending request")
		return 1
	}
	defer resp.Body.Close()
	fmt.Println(resp.Status, resp.StatusCode)
	if resp.StatusCode == http.StatusOK {
		return 0
	} else {
		return 1
	}
}

// Return
// url = "http://xxx": success
//       "": failed
func storeImage() (urlstring string) {
	// Return value
	urlstring = ""

	// Read file
	b, err := ioutil.ReadFile("Hydrangeas.jpg")
	if err != nil {
		fmt.Println(err, "in reading file")
		return
	}

	// Vernon debug
	fmt.Println("File length:", len(b))

	// Send request
	resp, err := http.Post(ItemURL+"storeImage", "image/jpeg", bytes.NewReader(b))
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()
	fmt.Println(resp.Status, resp.StatusCode)
	if resp.StatusCode != http.StatusCreated {
		// Get data from body
		b, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			fmt.Println(err, "in reading body")
			return
		}
		fmt.Printf("%s\n", b)
		return
	}
	url, err := resp.Location()
	if err != nil {
		fmt.Println(err, "in getting location from response")
		return
	}
	urlstring = url.String()
	fmt.Println("Location is", urlstring)

	return
}

// func main() {
// 	key := "ag9zfnRlc3RnY3NzZXJ2ZXJyJAsSBEl0ZW0iCUl0ZW0gUm9vdAwLEgRJdGVtGICAgICQ3Z4KDA"
// 	queryItem(key);
// 	fmt.Println("========================================")
// 	updateItem(key);
// 	fmt.Println("========================================")
// 	queryItem(key);
// }

func main() {
	var num int = 1
	var r int
	var key string

	// Random seed
	rand.Seed(time.Now().Unix())

	// Test suite
	fmt.Println("========================================")
	fmt.Println("Store an image")
	fmt.Println("========================================")
	imageUrlString := storeImage()
	if imageUrlString == "" {
		fmt.Println("Store an image failed")
		return
	} else {
		fmt.Println("Store an image " + imageUrlString)
	}

	fmt.Println("========================================")
	fmt.Printf("Store %d items of the image\n", num)
	fmt.Println("========================================")
	for i := 1; i <= num; i++ {
		r, key = storeItem(imageUrlString)
		if r != 0 {
			fmt.Printf("Store item %d failed\n", i)
			return
		} else {
			fmt.Printf("Store item %d in key %s\n", i, key)
		}
	}
// 	fmt.Println("========================================")
// 	fmt.Println("Query all items")
// 	fmt.Println("========================================")
// 	queryAll()

// 	fmt.Println("========================================")
// 	fmt.Println("Search items")
// 	fmt.Println("========================================")
// 	searchItem()

	fmt.Println("========================================")
	fmt.Println("Query the last item")
	fmt.Println("========================================")
	if queryItem(key) != 0 {
		fmt.Println("Failed to query item key", key)
		return
	} else {
		fmt.Println("Query item key", key, "successfully")
	}

	fmt.Println("========================================")
	fmt.Println("Update the last item")
	fmt.Println("========================================")
	if updateItem(key) != 0 {
		fmt.Println("Failed to update item key", key)
		return
	} else {
		fmt.Println("Update item key", key, "successfully")
	}

	fmt.Println("========================================")
	fmt.Println("Query the update item")
	fmt.Println("========================================")
	if queryItem(key) != 0 {
		fmt.Println("Failed to query item key", key)
		return
	} else {
		fmt.Println("Query item key", key, "successfully")
	}

	fmt.Println("========================================")
	fmt.Println("Delete the last item")
	fmt.Println("========================================")
	if deleteItem(key) != 0 {
		fmt.Println("Failed to delete item key", key)
		return
	} else {
		fmt.Println("Delete item key", key)
	}

// 	fmt.Println("========================================")
// 	fmt.Println("Delete all items")
// 	fmt.Println("========================================")
// 	if deleteAll() != 0 {
// 		fmt.Println("Delete failed")
// 		return
// 	} else {
// 		fmt.Println("Delete all")
// 	}
}
