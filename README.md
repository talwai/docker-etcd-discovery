# docker-etcd-discovery
Service Discovery experiment with Docker and Etcd

## Contents
├── etcd_client               // An example Go client performing service discovery via etcd watches
│   ├── Dockerfile
│   └── client.go
├── etcd_service_publisher    // Base components of the talwai/etcd_service_publisher image
│   ├── Dockerfile
│   ├── register_me.py        // Entrypoint script to register a service on Docker container startup
│   └── requirements.txt
└── test_service              // An example service registering itself with etcd using talwai/etcd_service_publisher as a base
    ├── Dockerfile
    └── Dockerfile.mustache   // A Mustache template useful as a starting point for microservices that must register with etcd


## HowTo

#### Run etcd
`$ docker run --name etcd_service quay.io/coreos/etcd`

#### Run test_service
```bash
$ docker build test_service
<our_image_id>
$ docker run -e ETCD_HOST=${ETCD_HOST} -e ETCD_PORT=${ETCD_PORT} --name test_service <our_image_id>
```

#### Run etcd-client
```bash
$ docker build etcd_client
<our_image_id>
$ docker run -e ETCD_HOST=${ETCD_HOST} -e ETCD_PORT=${ETCD_PORT} --name go_client <our_image_id>
```

For more info: see blogpost here 
  
