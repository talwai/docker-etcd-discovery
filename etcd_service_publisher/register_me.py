from __future__ import print_function

import time
import os
import sys
import etcd

ETCD_HOST = os.environ.get('ETCD_HOST')
ETCD_PORT = int(os.environ.get('ETCD_PORT'))

def _get_ip_addr():
    return ''

DISCOVERABLE_URL = os.environ.get('DISCOVERABLE_URL', _get_ip_addr())

def _get_client():
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
    print('MY ADDRESS: ', '{}:{}'.format(DISCOVERABLE_URL, service_port))
    print('ETCD stored address:', [n.value for n in cl.read(service_name).children])
