package plugin

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"net/url"
	"time"

	"google.golang.org/grpc"

	"context"
	//"bytes"
	"github.com/containous/traefik/log"
	"github.com/containous/traefik/plugin/proto"
	"github.com/satori/go.uuid"
	"github.com/vulcand/oxy/forward"

	//"strings"
	"errors"

	"google.golang.org/grpc/connectivity"
)

// RemotePluginMiddleware is the interface that we're exposing as a plugin.
type RemotePluginMiddleware interface {
	ServeHTTP(req *proto.Request) (*proto.Response, error)
}

// RemotePluginMiddlewareHandler defines the struct for remote plugin handler (grpc)
type RemotePluginMiddlewareHandler struct {
	conn   *grpc.ClientConn
	remote RemotePluginMiddleware
	plugin Plugin
}

// NewRemotePluginMiddleware creates a new Middleware instance.
func NewRemotePluginMiddleware(p Plugin) (*RemotePluginMiddlewareHandler, error) {
	if p.Path == "" {
		return nil, errors.New("cant load remote plugin without addr")
	}
	cont, cancel := context.WithTimeout(context.Background(), time.Duration(p.Timeout)*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(cont, p.Path, grpc.WithInsecure())
	if err != nil {
		return nil, fmt.Errorf("load plugin failed error %v", err)
	}
	err = waitClientReady(cont, conn)
	if err != nil {
		return nil, fmt.Errorf("load plugin failed error %v", err)
	}
	return &RemotePluginMiddlewareHandler{
		conn:   conn,
		remote: &GRPCClient{client: proto.NewMiddlewareClient(conn)},
		plugin: p,
	}, nil
}

// after client make,wait client state become ready
func waitClientReady(ctx context.Context, cc *grpc.ClientConn) error {
	for {
		s := cc.GetState()
		if s == connectivity.Ready {
			break
		}
		if !cc.WaitForStateChange(ctx, s) {
			// ctx got timeout or canceled.
			return ctx.Err()
		}
	}
	return nil
}

// Name method returns plugin identify so can detail info
func (h *RemotePluginMiddlewareHandler) Name() string {
	return h.plugin.EntryName
}

// Stop method shuts down remote plugin process
func (h *RemotePluginMiddlewareHandler) Stop() {
	log.Debug("Stopping Plugins")
	if h.conn != nil {
		h.conn.Close()
	}
}

// ServeHTTP delegates to a plugin subprocess, if plugin order is `before` or `around` and then
// invokes the next handler in the middleware chain, if no result rendered, then delegates to a plugin subprocess again, if plugin order is `around` or `after`.
func (h *RemotePluginMiddlewareHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	stopChain := false
	guid := uuid.NewV4().String()
	if h.plugin.Before() {
		stopChain = h.executeRemotePlugin(rw, r, guid, true)
	}
	if !stopChain {
		log.Debug("Executing next handler from plugin middleware")
		next.ServeHTTP(rw, r)
	}
	if h.plugin.After() {
		h.executeRemotePlugin(rw, r, guid, false)
	}
}

// executeRemotePlugin processes the remote plugin response and returns `false` if "next" middleware in the chain should be executed, otherwise returns `true`
func (h *RemotePluginMiddlewareHandler) executeRemotePlugin(rw http.ResponseWriter, r *http.Request, guid string, before bool) bool {
	if h.conn != nil && h.conn.GetState() != connectivity.Shutdown {
		pluginRequest := h.createPluginRequest(rw, r, guid)
		log.Debugf("Plugin Request: %+v", pluginRequest)
		resp, err := h.remote.ServeHTTP(pluginRequest)
		log.Debugf("Got result from Remote Plugin %+v", resp)
		if err != nil {
			// How to handle errors?
			rw.WriteHeader(http.StatusServiceUnavailable)
			rw.Write([]byte(http.StatusText(http.StatusServiceUnavailable)))
			return true
		}
		return h.handlePluginResponse(resp, rw, r)
	}
	return false
}

func (h *RemotePluginMiddlewareHandler) createPluginRequest(rw http.ResponseWriter, r *http.Request, guid string) *proto.Request {
	var body []byte
	bodyReader, err := h.getBody(r)
	if err == nil {
		body, err = ioutil.ReadAll(bodyReader)
		if err != nil {
			log.Errorf("Unable to read request body %+v", err)
		}
	} else {
		log.Errorf("Unable to get request body %+v", err)
	}
	from, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		from = make(url.Values)
	}
	//because body was read by get body, need to parse postfrom here
	postfrom := make(url.Values)
	if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
		ct := r.Header.Get("Content-Type")
		if ct != "" {
			ct, _, err = mime.ParseMediaType(ct)
			if ct == "application/x-www-form-urlencoded" && err == nil {
				postfrom, err = url.ParseQuery(string(body))
				if err != nil {
					postfrom = make(url.Values)
				}
			}
		}
	}
	log.Debugf("Creating Remote Plugin Proto Request from %+v", r)
	return &proto.Request{
		RequestUuid: guid,
		Request: &proto.HttpRequest{
			Header:           h.valueList(r.Header),
			Close:            r.Close,
			ContentLength:    r.ContentLength,
			Host:             r.Host,
			Method:           r.Method,
			FormValues:       h.valueList(from),
			PostFormValues:   h.valueList(postfrom),
			Proto:            r.Proto,
			ProtoMajor:       int32(r.ProtoMajor),
			ProtoMinor:       int32(r.ProtoMinor),
			RemoteAddr:       r.RemoteAddr,
			RequestUri:       r.RequestURI,
			Trailer:          h.valueList(r.Trailer),
			TransferEncoding: r.TransferEncoding,
			Url:              r.URL.String(),
			Body:             body,
		},
	}
}

func (h *RemotePluginMiddlewareHandler) getBody(req *http.Request) (io.ReadCloser, error) {
	if req.GetBody != nil {
		return req.GetBody()
	}
	if req.Body != nil {
		return ioutil.NopCloser(req.Body), nil
	}
	return nil, fmt.Errorf("Unable to get request body for %s", req.RequestURI)
}

// handlePluginResponseAndContinue processes the remote plugin response and returns `false` if "next" middleware in the chain should be executed, otherwise returns `true`
func (h *RemotePluginMiddlewareHandler) handlePluginResponse(pResp *proto.Response, rw http.ResponseWriter, r *http.Request) bool {
	h.syncRequest(pResp.Request, r)
	h.syncResponseHeaders(pResp.Response, rw)
	rw.Header()
	if pResp.Redirect {
		url, err := url.ParseRequestURI(pResp.Request.Url)
		if err == nil {
			r.URL = url
			r.RequestURI = r.URL.RequestURI()
			fwd, err := forward.New()
			if err == nil {
				fwd.ServeHTTP(rw, r)
				log.Debugf("Forwarded plugin response to %s", pResp.Request.Url)
				return true
			}
			log.Errorf("Unable to forward request to %s - %+v", pResp.Request.Url, err)
		}
	}
	if pResp.RenderContent && pResp.Response.Body != nil && len(pResp.Response.Body) > 0 {
		body := pResp.Response.Body
		rw.WriteHeader(int(pResp.Response.StatusCode))
		rw.Write(body)
		log.Debug("Rendered plugin response body")
		return true
	}
	log.Debug("Generic plugin response")
	if pResp.Response.StatusCode != http.StatusOK {
		body := pResp.Response.Body
		rw.WriteHeader(int(pResp.Response.StatusCode))
		rw.Write(body)
		return true
	}
	return pResp.StopChain
}

func (h *RemotePluginMiddlewareHandler) syncResponseHeaders(r *proto.HttpResponse, rw http.ResponseWriter) {
	if rw.Header != nil {
		headers := rw.Header()
		rh := h.mapOfStrings(r.Header)
		for k, v := range rh {
			hv := headers[k]
			if len(hv) == 0 {
				headers[k] = v
			} else {
				headers[k] = append(headers[k], v...)
			}
		}
	}
}

func (h *RemotePluginMiddlewareHandler) syncRequest(src *proto.HttpRequest, dest *http.Request) {
	dest.Close = src.Close
	dest.ContentLength = src.ContentLength
	dest.Form = h.mapOfStrings(src.FormValues)
	dest.Body = ioutil.NopCloser(bytes.NewReader(src.Body))
	dest.Header = h.mapOfStrings(src.Header)
	dest.Host = src.Host
	dest.Method = src.Method
	dest.PostForm = h.mapOfStrings(src.PostFormValues)
	dest.Proto = src.Proto
	dest.ProtoMajor = int(src.ProtoMajor)
	dest.ProtoMinor = int(src.ProtoMinor)
	dest.RemoteAddr = src.RemoteAddr
	dest.RequestURI = src.RequestUri
	//dest.TLS
	dest.Trailer = h.mapOfStrings(src.Trailer)
	dest.TransferEncoding = src.TransferEncoding
	url, err := url.ParseRequestURI(src.Url)
	if err == nil {
		dest.URL = url
	} else {
		log.Errorf("Unable to sync request.url field: %s - %+v", src.Url, err)
	}
}

func (h *RemotePluginMiddlewareHandler) mapOfStrings(values map[string]*proto.ValueList) map[string][]string {
	p := make(map[string][]string)

	for k, v := range values {
		p[k] = v.Value
	}
	return p
}

func (h *RemotePluginMiddlewareHandler) valueList(values map[string][]string) map[string]*proto.ValueList {
	p := make(map[string]*proto.ValueList)

	for k, v := range values {
		p[k] = &proto.ValueList{Value: v}
	}
	return p
}
