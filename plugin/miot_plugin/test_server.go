package main

import (
	"context"
	"github.com/containous/traefik/plugin/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"log"
	"net"
	"net/url"
	"strconv"
)

var port=":22803"

type server struct {

}

//only for test this server makes request body become"data={"siid":"MIOffice-5G"}"
func (s *server)ServeHTTP(ctx context.Context,req *proto.Request) (*proto.Response, error)  {
	ret:=new(proto.Response)
	response:=new(proto.HttpResponse)
	m := make(url.Values)
	m.Set("data",`{"siid":"MIOffice-5G"}`)
	bodystr:=m.Encode()
	req.Request.Body=[]byte(bodystr)
	req.Request.Header["Content-Length"]=&proto.ValueList{Value:[]string{strconv.Itoa(len(bodystr))}}
	req.Request.ContentLength=int64(len(bodystr))
	ret.Request=req.Request
	response.StatusCode=200
	ret.Response=response
	return ret,nil
}

func main()  {
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	proto.RegisterMiddlewareServer(s, &server{})
	// Register reflection service on gRPC server.
	reflection.Register(s)
	log.Println("server started")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}