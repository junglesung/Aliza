package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Item struct {
	Id         string    `json:"id"`
	People     int       `json:"people"`
	Attendant  int       `json:"attendant"`
	Image      string    `json:"image"`
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
	s += fmt.Sprintln("Id:", b.Id)
	s += fmt.Sprintln("People:", b.People)
	s += fmt.Sprintln("Attendant:", b.Attendant)
	s += fmt.Sprintln("Image:", b.Image)
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
	var items map[string]Item = make(map[string]Item)
	if resp.StatusCode == http.StatusOK {
		// Decode as JSON
		if err := json.Unmarshal(body, &items); err != nil {
			fmt.Println(err, "in decoding JSON")
			return
		}
		for i, v := range items {
			fmt.Println("-------------------------------")
			fmt.Println("Key:", i)
			fmt.Println(v)
		}
		fmt.Println("Total", len(items), "items")
	} else {
		// Decode as text
		fmt.Printf("%s", body)
	}
}

func queryItem() {
	// Make URL
	var u *url.URL
	var err error
	if u, err = url.ParseRequestURI(ItemURL + "items"); err != nil {
		fmt.Println(err, "in making URL")
		return
	}
	var q url.Values = u.Query()
	q.Add("People", rand.Intn(ItemMaxPeople - ItemMinPeople) + ItemMinPeople)
	u.RawQuery = q.Encode()

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
	var items map[string]Item = make(map[string]Item)
	if resp.StatusCode == http.StatusOK {
		// Decode as JSON
		if err := json.Unmarshal(body, &items); err != nil {
			fmt.Println(err, "in decoding JSON")
			return
		}
		for i, v := range items {
			fmt.Println("-------------------------------")
			fmt.Println("Key:", i)
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
		People:     rand.Intn(rand.Intn(ItemMaxPeople - ItemMinPeople) + ItemMinPeople),
		Attendant:  1,
		Image:      imgUrl,
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
	if resp.StatusCode == http.StatusOK {
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
// int = 0: success
//       1: failed
// string is the new item's unique key
func storeImage() (r int) {
	// Return value
	r = 0

	// Read file
	b, err := ioutil.ReadFile("Hydrangeas.jpg")
	if err != nil {
		fmt.Println(err, "in reading file")
		r = 1
		return
	}

	// Vernon debug
	fmt.Println("File length:", len(b))

	// Send request
	resp, err := http.Post(ItemURL+"storeImage", "image/jpeg", bytes.NewReader(b))
	if err != nil {
		fmt.Println(err)
		r = 1
		return
	}
	defer resp.Body.Close()
	fmt.Println(resp.Status, resp.StatusCode)
	if resp.StatusCode != http.StatusCreated {
		// Get data from body
		b, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			fmt.Println(err, "in reading body")
			r = 1
			return
		}
		fmt.Printf("%s\n", b)

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
	// tokens := strings.Split(url.Path, "/")
	// var keyIndexInTokens int = 0
	// for i, v := range tokens {
	// 	if v == "items" {
	// 		keyIndexInTokens = i + 1
	// 	}
	// }
	// if keyIndexInTokens >= len(tokens) {
	// 	fmt.Println("Key is not given")
	// 	return
	// }
	// key = tokens[keyIndexInTokens]
	// if key == "" {
	// 	fmt.Println("Key is empty")
	// 	return
	// }
	return
}

func main() {
	storeImage()
	// // Random seed
	// rand.Seed(time.Now().Unix())

	// // Test suite
	// fmt.Println("========================================")
	// if storeTen() != 0 {
	// 	fmt.Println("Store items failed")
	// 	return
	// } else {
	// 	fmt.Println("Store 10 items")
	// }
	// fmt.Println("========================================")
	// r, key := storeItem()
	// if r != 0 {
	// 	fmt.Println("Store a item failed")
	// 	return
	// } else {
	// 	fmt.Println("Store a item in key", key)
	// }
	// fmt.Println("========================================")
	// queryAll()
	// fmt.Println("========================================")
	// queryItem()
	// fmt.Println("========================================")
	// if deleteItem(key) != 0 {
	// 	fmt.Println("Failed to delete item key", key)
	// 	return
	// } else {
	// 	fmt.Println("Delete item key", key)
	// }
	// fmt.Println("========================================")
	// if deleteAll() != 0 {
	// 	fmt.Println("Delete failed")
	// 	return
	// } else {
	// 	fmt.Println("Delete all")
	// }
}
