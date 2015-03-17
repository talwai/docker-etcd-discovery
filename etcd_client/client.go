package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"time"
)

type etcdNode struct {
	Key   string
	Value string
}

type etcdDirectoryNode struct {
	Key   string
	Dir   bool
	Nodes []map[string]string
	Value string
}

type etcdDirectory struct {
	Node etcdDirectoryNode
}

type etcdWatchResponse struct {
	Action string
	Node   etcdNode
}

func selectRandomChild(m etcdDirectory) *etcdNode {
	lenChildren := len(m.Node.Nodes)

	if lenChildren > 0 {
		randNode := m.Node.Nodes[rand.Intn(lenChildren)]
		return &etcdNode{string(randNode["key"]),
			string(randNode["value"])}
	}

	return nil
}

func waitEtcdWatchResponse(watchUrl string) *etcdWatchResponse {
	res, er := http.Get(watchUrl)
	serror(er)

	defer res.Body.Close()
	var m etcdWatchResponse

	err := json.NewDecoder(res.Body).Decode(&m)
	serror(err)

	return &m
}

func getNewAddress(etcdUrl string) string {
	res, er := http.Get(etcdUrl)
	serror(er)

	defer res.Body.Close()
	var m etcdDirectory

	err := json.NewDecoder(res.Body).Decode(&m)
	serror(err)

	node := selectRandomChild(m)

	if node == nil {
		return "nowhere"
	}

	return node.Value
}

func serror(err error) {
	if err != nil {
		fmt.Println("%s", err)
	}
}

func watchServiceAddrChannel(etcdWatchUrl string, etcdGetUrl string) <-chan string {
	watchChan := make(chan string)
	go func() {
		for {
			watchResponse := waitEtcdWatchResponse(etcdWatchUrl)
			fmt.Println("Watch triggered: ", watchResponse.Action, watchResponse.Node.Key)
			addr := getNewAddress(etcdGetUrl)
			watchChan <- addr
		}
	}()
	return watchChan
}

func setOnChange(in <-chan string, toSet *string) {
	for {
		*toSet = <-in
	}
}

func main() {
	ETCD_HOST := os.Getenv("ETCD_HOST")
	ETCD_PORT := os.Getenv("ETCD_PORT")
	serviceLookupKey := "/backends/kak_service"

	fmt.Println("Listening for service with role", serviceLookupKey)
	etcdGetUrl := fmt.Sprintf("http://%s:%s/v2/keys%s",
		ETCD_HOST,
		ETCD_PORT,
		serviceLookupKey,
	)

	etcdWatchUrl := fmt.Sprintf("%s?wait=true&recursive=true",
		etcdGetUrl,
	)

	GLOBAL_SERVICE_ADDR := getNewAddress(etcdGetUrl)
	go setOnChange(watchServiceAddrChannel(
		etcdWatchUrl, etcdGetUrl), &GLOBAL_SERVICE_ADDR)

	for {
		fmt.Println("Current service lives at: ", GLOBAL_SERVICE_ADDR)
		time.Sleep(500 * time.Millisecond)
	}
}
