FROM alpine
 
USER root:root

RUN mkdir /app
RUN mkdir /app/config
RUN mkdir /run/secrets
RUN mkdir /run/secrets/tls
WORKDIR /app
 
ADD ./admission-control /app/admission-control
 
EXPOSE 8443

USER nobody:nogroup
 
ENTRYPOINT ./admission-control