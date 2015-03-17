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
