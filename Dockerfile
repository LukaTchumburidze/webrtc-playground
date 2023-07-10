FROM golang:1.20

ENV GO111MODULE=on
COPY . /home/application/
RUN go build

CMD ["ECHO by default image echoes, configuration should be provided by docker-compose"]