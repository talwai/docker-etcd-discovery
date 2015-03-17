FROM talwai/etcd_service_publisher:latest

ENV MY_SERVICE_NAME /backends/cache_service
ENV LISTENING_ON_PORT 5000

# Bundle app source
ADD . /src

# Expose
EXPOSE 5000

CMD python discovery/register_me.py ${MY_SERVICE_NAME} ${LISTENING_ON_PORT} && bash
