FROM python:2.7-alpine

ENV APPPATH /app

RUN mkdir -p "$APPPATH/bin" \
 && chmod -R 755 "$APPPATH"

WORKDIR $APPPATH

COPY cf-bigip-ctlr $APPPATH/bin
COPY python/ $APPPATH/python
COPY requirements.txt /tmp/requirements.txt

RUN apk --no-cache --update add --virtual pip-install-deps git && \
    pip install -r $APPPATH/python/k8s-runtime-requirements.txt && \
    apk del pip-install-deps

# Run the run application in the projects bin directory.
CMD [ "/app/bin/cf-bigip-ctlr" ]
