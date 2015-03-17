### Simulating service discovery in a development context with Docker and etcd

For a long time I've been itching to get out of the prescriptive structure given by full-stack frameworks like Rails and Django. I've found that structure useful and desirable while ramping-up, but mostly constraining for complex projects that attack a variety of problem domains. For my most recent side-project I decided to experiment with a microservice-oriented design. My interpretation of this was that each individual concern in the functioning of my web application would be isolated into its own standalone service, running out of a Docker container. A few rules I tried to impose on myself as I put this together were: 
- The principal of least knowledge: A single service should be able to specify a finite set of services it depends on and need not know or care about any other parts of the stack
- A service should solve one problem and solve it well. The architecture should allow for the service to be written in the language most suited to that problem domain
- Wherever possible, services should be stateless. One should be able to swap out any service for one with an identical contract, with minimal adverse effect on clients.
- As a corollary to the above, once a host has been spun up, it should be considered immutable. It should be easy enough to destroy and recreate a service that the urge to patch infrastructure on-the-fly can be summarily avoided. This would also greatly ease the complexity of deployments
- Recreating such an expansive distributed architecture in a development context should be easy and painless

These rules bring up a couple of key challenges that anyone building an app around microservices will be faced with. The first is arriving at a standard for communication between services. Martin Fowler's [seminal article on microservices](martinfowler.com/articles/microservices.html) stresses that the core paradigm for choosing your communication layer should be 'Smart Endpoints and Dumb Pipes.' Broadly, this means that the plumbing in between your services should do little more than relay a message from point A to point B, and not overly concern itself with routing, load balancing or choreography. I briefly looked at running a lightweight message bus like [ZeroMQ](http://zeromq.org/) through my system, where clients would publish requests, and services would read them, process them and publish the results to an appropriate namespace. Exemplary distributed applications like [Saltstack](http://www.saltstack.com) have used this setup to great effect. But as a relative newcomer to distributed architectures, I decided to rely on good old HTTP. HTTP has ubiquitous library support and so it's never the case that you would have to opt against using the best language for the job, just because its bindings to the messaging layer are not mature enough. It's also easier to reason about for those more familiar with a standard client-server programming model, as I was.

The second common issue with microservices is handling service discovery. How does a client know where the services it depends on live, and how can it be assured that this information is up-to-date? Given our aforementioned commitment to immutable infrastructure, we cannot make the assumption that service XYZ will continue to live at `some_address.domain.com` for perpetuity. At some point the host will fail, and must be swapped for a new host with a new address. How can our system react to this change, gracefully and transparently?

At the heart of any service discovery solution is a distributed 'phone book' of sorts, that holds identifying information about every individual service in the cluster. A service publishes details about itself to the phone book on startup; clients of the service then ask the phone book for the location of the service when they need to use it. This broadly splits things up into a registration procedure and a discovery procedure. In this post I will look at a Docker-friendly solution to this problem using `etcd` as the 'phone book' i.e. the backing key-value store.

#### Why etcd?
I found a [great article](http://www.simplicityitself.com/learning/getting-started-microservices/service-discovery-overview/) evaluating the pros and cons of various key-value stores as backends to service discovery. I narrowed my options down to [Consul](http://www.consul.io) and [etcd](https://github.com/coreos/etcd) both of which offer rich API semantics over HTTP, which I considered a must. Admittedly, my eventual choice of `etcd` was driven by brand recognition of its maintainers more than anything else - `etcd` is built at CoreOS which was an early champion of application containers, and this gave me some peace of mind given that I was assembling my stack around Docker. Starting over, I can see myself favoring Consul because it is more purpose-built for the service discovery problem (and also [very easily Dockerized](https://github.com/progrium/docker-consul)). 

#### Service Registration
The etcd server can be easily spoken to over HTTP, and this is made even more convenient by the handful of language-specific client libraries. Assuming we have etcd running on `localhost:4001` we can set a key simply with:
```curl -L http://127.0.0.1:4001/v2/keys/foo -XPUT -d value="bar"```

In Python, the [python-etcd](https://github.com/jplana/python-etcd) client gives you a really convenient syntax to do the same:
```
>>> import etcd
>>> client = etcd.Client(host='127.0.0.1', port=4001)
>>> client.write('/foo', 'bar')                        
<class 'etcd.EtcdResult'>({'newKey': True, 'raft_index': 33045, '_children': [], 'createdIndex': 20, 'modifiedIndex': 20, 'value': u'bar', 'etcd_index': 20, 'expiration': None, 'key': u'/foo', 'ttl': None, 'action': u'set', 'dir': False})
>>> client.read('/foo').value
u'bar'
```
etcd also allows the concept of directories, which allows you to create and query descriptive nested namespaces:
```
>>> client.write('/backends/cache_services/0', 'somehost.somedomain.com')
>>> client.write('/backends/cache_services/1', 'otherhost.otherdomain.com')
>>> [(child.key, child.value) for child in client.read('/backends/cache_services').children]
[(u'/backends/cache_services/0', u'somehost.somedomain.com'), (u'/backends/cache_services/1', u'otherhost.otherdomain.com')]
>>> client.write('/backends/cache_services', 'thirdhost.thirddomain.com', append=True)
<class 'etcd.EtcdResult'>({'newKey': True, 'raft_index': 39844, '_children': [], 'createdIndex': 23, 'modifiedIndex': 23, 'value': u'third', 'etcd_index': 23, 'expiration': None, 'key': u'/backends/cache_services/23', 'ttl': None, 'action': u'create', 'dir': False})
>>> [(child.key, child.value) for child in client.read('/backends/cache_services').children]
[(u'/backends/cache_services/0', u'somehost.somedomain.com'), (u'/backends/cache_services/1', u'otherhost.otherdomain.com'), (u'/backends/cache_services/23', u'thirdhost.thirddomain.com')]
```

The above example gives a good model for how our service registration solution might work. If we register services under a strictly-defined directory depending on the role played by the service, it gives clients a clean way to query for a service by role. We will see this querying in action later on, when we move to the discovery procedure.
We also see two possible ways to write to an etcd directory. The first is to provide a direct key for the service to be stored under e.g `/backends/cache_services/0`. The second is to do a `POST` to the directory namespace, which allows us to take advantage of etcd in-order keys. Observe the call to `client.write('/backends/cache_services', 'thirdhost.thirddomain.com', append=True)`. With the `append` kwarg, `client.write` runs a `POST` in the background. This creates an identifier that is guaranteed to be unique to the `/backends/cache_services` directory, and also `>=` the last such identifier created. This is useful in the service discovery context because it removes the need for hardcoding identifiers. It allows services to simply register themselves within a role-specific namespace without worrying about clashes. For example the `/backends/cache` directory can be `POST`ed to repeatedly to create the unique keys `/backends/cache/0`, `/backends/cache/1` et cetera. (Note: from the etcd docs, "key names use the global etcd index, so the next key can be more than `previous + 1`")

Let's wrap some of the etcd methods we've walked through so far into a small script that handles the registration:

`register_me.py`
```
from __future__ import print_function

import os
import sys
import etcd
import netifaces

ETCD_HOST = os.environ.get('ETCD_HOST')
ETCD_PORT = int(os.environ.get('ETCD_PORT'))

def _get_ip_addr():
	'''
	Return the IP address of the host
	'''
    addrs = netifaces.ifaddresses('eth0')
    inet_addr = addrs[netifaces.AF_INET][0]
    return inet_addr['addr']

DISCOVERABLE_URL = os.environ.get('DISCOVERABLE_URL', _get_ip_addr())

def _get_client():
	'''
	Return an HTTP client to the service registry
	'''
    return etcd.Client(host=ETCD_HOST, port=ETCD_PORT)

def register_me(name, port, **kwargs):
    client = _get_client()
    full_addr = '{h}:{p}'.format(h=DISCOVERABLE_URL, p=port)
    client.write(name, full_addr, **kwargs)

if __name__ == '__main__':
    if len(sys.argv) is not 3:
        raise Exception('<name> and <port> of service required!')

    service_name = sys.argv[1]
    service_port = sys.argv[2]
    register_me(service_name, service_port, append=True)

    # Test that registration 'took'
    cl = _get_client()
    print('MY ADDRESS: ', DISCOVERABLE_URL)
    print('ETCD stored address:', [n.value for n in cl.read(service_name).children])
``` 

Here's an example run, assuming etcd is running on `127.0.0.1:4001`:
```
$ ETCD_HOST=127.0.0.1 ETCD_PORT=4001 DISCOVERABLE_URL=my.domain.com python register_me.py /backends/cache_service 8001

MY ADDRESS: my.domain.com:8001
ETCD stored address: ['my.domain.com:8001']
```

The script is pretty straightforward, it reads in the etcd location, and the URL that the host wants to be discovered as, and registers itself under the appropriate namespace
(Note: I rely on the [netifaces](https://pypi.python.org/pypi/netifaces) Python library as an OS-agnostic way to query network interfaces when there is no `DISCOVERABLE_URL` provided.  My implementation of `_get_ip_addr` inspects the `eth0` interface to determine the location at which this host is addressable. It is far from robust, but has worked thus far, and it's on my TODO list to come back to it when I understand UNIX network interfaces and Docker networking paradigms better.)

While this script does the trick on service startup, we're not done yet. A robust service registration solution will account for the fact that services can fail and terminate at any time. If a service goes down, but its address continues to live in our service registry, client requests will be routed to a host that is not capable of handling them. We can get around this problem by configuring a Time-To-Live (TTL) on keys that we set. The point of TTL on values in a K/V store is to ensure that values don’t get stale. It is better to repeatedly set a value with up to date info than to risk retrieving a stale value and using it as if it were active. etcd allows you to easily set TTL via a url param, and this allows us to configure service 'heartbeats'. What this means is that a service with a short TTL re-registers itself every N seconds, ensuring that the directory space for a particular service invariably holds only hosts that are alive and well( within an N second margin of error)

Here's what such a heartbeat setup might look like:
```
from time import sleep
TTL = 4 # seconds

while True:
	register_me(service_name, service_port, append=True, ttl=TTL)
	if not main_process_is_running():
	    sys.exit(-1)
	time.sleep(TTL - 1)
```

Ideally this heartbeat loop would run as a daemon, with some sort of communication set up that allows the loop to know that the main process is still running. Fully implementing this heartbeat is left as an exercise for the reader for now, but I plan to come back to it soon.

#### Service Discovery
Now let's look at the discovery side of the solution. We spoke earlier about individual nodes being registered under strict namespaces based on their role. The client requires knowledge of the location of the etcd node/cluster, and the namespace of each service it depends on. If I require a `db` service and a `compute` service to operate, then I will be looking for these under `/backends/db` and `backends/compute`. 

It's easy enough for the client to query etcd for these values on startup and store them locally for the duration of its run. But this is problematic if these values ever change, in which case the only way for the new config to 'take' is to restart the client. Thankfully etcd gives your client an easy way to reconfigure itself on the fly, via the [watch](https://coreos.com/docs/distributed-configuration/etcd-api/#key-space-operations) feature.

Starting a `watch` on an etcd namespace over HTTP sets up a long-polling connection that responds when that namespace has been updated. Hence if the language of your client supports callbacks or callback-esque constructs, then you can update service location data as the need arises. Here's an example of how you could set up such a client in Go:
`client.go`
```
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
	serviceLookupKey := "/backends/cache_service"

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
```

Here's the gist of what happens above. We're interested in the services under the directory `/backends/cache_service`. First, as a bootstrapping step, we call `getNewAddress` which queries etcd for a random node within the required directory. The randomness is a fairly naive way to assure a reasonably even distribution across nodes. A more sophisticated solution could swap out the `selectRandomChild` step for a step that would inspect metadata about individual hosts before selecting one of them. For example, the heartbeat on the registration side could also send up information about the current load average on the host. On the discovery side, the client could then point its requests to the service that has the lowest load out of those for a given role.

Then we setup a watch on the `/backends/cache_service` directory so that we can be notified of any changes to it. Etcd watches can be made recursive in nature in order to monitor the contents of an entire directory, and this is what we do here with the `recursive=true` URL param. We make a channel which is written to with an  whenever a watch is triggered on our etcd directory. The value written is an updated address randomly chosen from the new service candidates. The goroutine `setOnChange` updates `GLOBAL_SERVICE_ADDR` so that it always contains a valid location for the client to point requests to.

Let's observe this script in action:
```
$ ETCD_HOST=127.0.0.1 ETCD_PORT=4001 go run client.go
Listening for service with role /backends/cache_service
Current service lives at:  nowhere
Current service lives at:  nowhere
...
```

No services have been registered under `/backends/cache_service` yet, and so our etcd lookup fails. In other terminal tab, lets `POST` to our etcd instance running on `127.0.0.1:4001`

```
$ curl -XPOST http://127.0.0.1:4001/v2/keys/backends/cache_service -d value='host.mydomain.com'
{"action":"create","node":{"key":"/backends/cache_service/89","value":"host.mydomain.com","modifiedIndex":89,"createdIndex":89}}
```
If you look back over the tab running the go client, you'll see the output updated:

```
Watch triggered: create /backends/cache_service/89
Current service lives at: host.mydomain.com
Current service lives at: host.mydomain.com
```

Let's add another entry to `/backends/cache_service`, then delete the first one.
```
$ curl -XPOST http://127.0.0.1:4001/v2/keys/backends/cache_service -d value='host2.mydomain.com'
{"action":"create","node":{"key":"/backends/cache_service/90","value":"host2.mydomain.com","modifiedIndex":90,"createdIndex":90}}
$ curl -L http://127.0.0.1:4001/v2/keys/backends/cache_service | python -mjson.tool
{
    "action": "get",
    "node": {
        "createdIndex": 89,
        "dir": true,
        "key": "/backends/cache_service",
        "modifiedIndex": 89,
        "nodes": [
            {
                "createdIndex": 89,
                "key": "/backends/cache_service/89",
                "modifiedIndex": 89,
                "value": "host.mydomain.com"
            },
            {
                "createdIndex": 90,
                "key": "/backends/cache_service/90",
                "modifiedIndex": 90,
                "value": "host2.mydomain.com"
            }
        ]
    }
}
$ curl -XDELETE http://127.0.0.1:4001/v2/keys/backends/cache_service/89
{"action":"delete","node":{"key":"/backends/cache_service/89","modifiedIndex":91,"createdIndex":89},"prevNode":{"key":"/backends/cache_service/89","value":"host.mydomain.com","modifiedIndex":89,"createdIndex":89}
```
Now the original service that was discovered by our Go client no longer has an entry in etcd. But another (presumably) identical one does. How has our client handled this change?

```
Watch triggered: create /backends/cache_service/90
Current service lives at: host.mydomain.com
Current service lives at: host.mydomain.com
...
Watch triggered:  delete /backends/cache_service/89
Current service lives at: host2.mydomain.com
Current service lives at: host2.mydomain.com
```

The first watch was for the initial `POST` of the `host2.mydomain.com` entry. Our client continued to point the same address, while there were two possible locations in the registry. The second watch was triggered by the `DELETE` on the entry containing `host.mydomain.com`. We can observe that our client gracefully reacted to this by switching over to the location `host2.mydomain.com`. If you play around with this setup a little more you'll see that most sequences of events that model real-world scenarios of service failures and restarts are handled gracefully by the client. `GLOBAL_SERVICE_ADDRESS` only points to `nowhere` when there are no entries under `/backends/cache_service`. In other scenarios, we can be completely reactive to dynamic changes in service configuration, with a very small amount of code.

#### Docker-izing
Now that we've modeled the registration and discovery, we can come back to the microservice framework I talked about at the start, where each individual service runs in a Docker container. For this framework to be usable and extensible, the semantics for registering a service container with etcd must be as Docker-friendly as possible. When a new service has to be introduced, the developer should have to spend as little time as possible on the boilerplate for registering the service with etcd, and focus instead on the business needs of the service.

Docker maintains a private IP network between all containers running on the same host. This network is useful for simulating a distributed architecture in a development context, and we can leverage it to test our service discovery solution from the comfort of our own machine. First we want to be assured that service registration happens consistently at container startup, with minimal boilerplate. After some iteration, I arrived at the idea of assembling a `publisher` Docker image, which would serve as the base image for any of our microservices. The contents of this image would be fairly minimal. It would consist of the Python dependencies needed by the `register_me` script that we discussed above, and the `register_me` script itself, in a pre-defined location. This would allow a Docker image built from this base to call out to a known module to register itself. Here's what the Dockerfile for the `talwai/etcd_service_publisher` image looks like:
```
FROM ubuntu:14.04

# Install Core deps
RUN apt-get update && apt-get install -y \
    libffi-dev \
    libssl-dev \
    python-dev \
    libc6 \
    libc-dev \
    g++ \
    build-essential \
    python-setuptools

# Install pip
RUN easy_install pip

RUN mkdir /discovery

# Install python modules and entrypoint for etcd registration
COPY requirements.txt /discovery/requirements.txt
RUN cd /discovery; pip install -r requirements.txt
COPY register_me.py /discovery/register_me.py
```

I use the `/discovery` directory to hold all registration concerns which, because of Docker's union filesystems, will be part of all child images as well.

Here's a Dockerfile for an example microservice `cache_service`, building off of the base `publisher` image
```
FROM talwai/etcd_service_publisher:latest

ENV MY_SERVICE_NAME /backends/cache_service
ENV LISTENING_ON_PORT 5000

# Bundle app source
ADD . /src

# Expose
EXPOSE 5000

CMD python discovery/register_me.py ${MY_SERVICE_NAME} ${LISTENING_ON_PORT} && python src/service.py
```
There is still some boilerplate code involved here. Mainly every Dockerfile must specify its role, and the port it will be listening on, which will be used to form the (key, value) pair for storage in etcd. Finally the `CMD` instruction must necessarily call the registration step first, prior to running the container's main process. But the good thing about boilerplate is that its ripe for templating. When generating this Dockerfile, a simple command line hook into a `mustache` template, will save you some headaches.

`Dockerfile.mustache`
```
FROM talwai/etcd_service_publisher:latest

#Add and install dependencies/ Perform setup here

ENV MY_SERVICE_NAME {{ service_type }}
ENV LISTENING_ON_PORT {{ service_port }}

CMD python discovery/register_me.py ${MY_SERVICE_NAME} ${LISTENING_ON_PORT} && {{ main_process }}
```
And with your renderer of choice e.g `pystache`

```
>>> import pystache
>>> renderer = pystache.Renderer()
>>> content = renderer.render_path('Dockerfile.mustache', {'service_type': '/backends/cache_service', 'service_port': '3001'....})
>>> with open('Dockerfile', 'w') as f: f.write(content)
```

(A work in progress right now is to wrap the above rendering into an interactive command-line utility a la `npm` and `bower` )

Once our Dockerfile is set up, all we have to do is buid the image, then run the container with 
`docker run -e ETCD_HOST=${ETCD_HOST} -e ETCD_PORT=${4001} --name test_service <our_image_id>` to register and run our containerized service.

We can setup a simple Dockerfile for our Go client as well
```
FROM golang:1.3-onbuild

CMD go run client.go
```
then build the image it and run it with:
`docker run -e ETCD_HOST=${ETCD_HOST} -e ETCD_PORT=${4001} --name go_client <our_image_id>`

What we see in the terminal, is the familiar output:
```
Current service lives at: 172.10.0.2:3001
Current service lives at: 172.10.0.2:3001
```
Try stopping and starting new containers running `test_service` and you can see the Go client re-configure itself on the fly. Hence from a developer's perspective, the time to spin up a new Docker container and have it registered and discoverable by clients is fairly short. They can focus on their implementation of the service itself, and trust that the plumbing between itself and `etcd` will be taken care of for them.

#### Next Steps
These are just the beginnings of my effort to create a solid, reproducible scaffolding for a microservices-based application. Ideally, the eventual outcome should be a framework that makes it at least half as easy for users to get up and running as Rails and Django do. I think there's room for conventions to develop around how things like service composition, and reverse proxying are handled. These are some of the topics I hope to tackle in upcoming blog posts. All the code referenced in this post can be found on this Github page: 
Looking forward to comments and feedback.

> Written with [StackEdit](https://stackedit.io/).
