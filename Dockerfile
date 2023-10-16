FROM golang:1.20

ENV GO111MODULE=on
COPY . /home/application/
WORKDIR /home/application/cmd
RUN go build

CMD ["ECHO by default image echoes, configuration should be provided by docker-compose"]