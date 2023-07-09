FROM golang:1.20

ENV GO111MODULE=on
COPY . /home/application/
RUN go build

CMD ["ECHO by defoult image echoes, configuration should be provided by docker-compose"]