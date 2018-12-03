package plugin

import (
	"fmt"
	"strconv"
	"strings"
)

// Flaeg Parsing Hooks

// Plugin defines a plugin configuration in traefik
type Plugin struct {
	EntryName string
	Path      string
	Type      string
	Order     string
	Timeout   int64
}

// Before returns true if plugin execution order is going to take place before the next handler, i.e "order" == 'before' or 'around'
func (p *Plugin) Before() bool {
	return p.Order == PluginBefore || p.Order == PluginAround
}

// After returns true if plugin execution order is going to take place after the next handler, i.e "order" == 'after' or 'around'
func (p *Plugin) After() bool {
	return p.Order == PluginAfter || p.Order == PluginAround
}

// Around returns true if plugin execution order is going to take place around the next handler, i.e "order" == 'around'
func (p *Plugin) Around() bool {
	return p.Order == PluginAround
}

// Constants defining Plugin types and orders
const (
	PluginGo     = "go"
	PluginNetRPC = "netrpc"
	PluginGrpc   = "grpc"

	PluginBefore = "before"
	PluginAfter  = "after"
	PluginAround = "around"
)

// Plugins defines a set of Plugin
type Plugins []Plugin

//Set Plugins from a string expression
func (p *Plugins) Set(str string) error {
	exps := strings.Fields(str)
	if len(exps) == 0 {
		return fmt.Errorf("bad Plugin format: %s", str)
	}
	for _, exp := range exps {
		field := strings.SplitN(exp, ":", 2)
		if len(field) != 2 {
			return fmt.Errorf("bad Plugin definition format: %s", exp)
		}
		tem := Plugin{}
		switch strings.ToLower(field[0]) {
		case "name":
			tem.EntryName = field[1]
		case "path":
			tem.Path = field[1]
		case "type":
			tem.Type = field[1]
		case "order":
			tem.Order = field[1]
		case "timeout":
			timeout, err := strconv.ParseInt(field[1], 10, 64)
			if err == nil {
				tem.Timeout = timeout
			}
		}
		*p = append(*p, tem)
	}
	return nil
}

//Get returns Plugins instances
func (p *Plugins) Get() interface{} {
	return []Plugin(*p)
}

//String returns Plugins formated in string
func (p *Plugins) String() string {
	if len(*p) == 0 {
		return ""
	}
	var result []string
	for _, pp := range *p {
		result = append(result, pp.Type+"|"+pp.Path)
	}
	return strings.Join(result, ",")
}

//SetValue sets Plugins into the parser
func (p *Plugins) SetValue(val interface{}) {
	*p = Plugins(val.(Plugins))
}

// Type exports the Plugins type as a string
func (p *Plugins) Type() string {
	return "plugins"
}
