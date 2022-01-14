package main

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/rerpc/rerpc"
)

const (
	contextPackage = protogen.GoImportPath("context")
	rerpcPackage   = protogen.GoImportPath("github.com/rerpc/rerpc")
	httpPackage    = protogen.GoImportPath("net/http")
	protoPackage   = protogen.GoImportPath("google.golang.org/protobuf/proto")
	stringsPackage = protogen.GoImportPath("strings")
	errorsPackage  = protogen.GoImportPath("errors")
	cstreamPackage = protogen.GoImportPath("github.com/rerpc/rerpc/callstream")
	hstreamPackage = protogen.GoImportPath("github.com/rerpc/rerpc/handlerstream")
)

var (
	contextContext          = contextPackage.Ident("Context")
	contextCanceled         = contextPackage.Ident("Canceled")
	contextDeadlineExceeded = contextPackage.Ident("DeadlineExceeded")
	errorsIs                = errorsPackage.Ident("Is")
)

func deprecated(g *protogen.GeneratedFile) {
	comment(g, "// Deprecated: do not use.")
}

func generate(gen *protogen.Plugin, file *protogen.File, separatePackage bool) *protogen.GeneratedFile {
	if len(file.Services) == 0 {
		return nil
	}
	filename := file.GeneratedFilenamePrefix + "_rerpc.pb.go"
	var path protogen.GoImportPath
	if !separatePackage {
		path = file.GoImportPath
	}
	g := gen.NewGeneratedFile(filename, path)
	preamble(gen, file, g)
	content(file, g)
	return g
}

func protocVersion(gen *protogen.Plugin) string {
	v := gen.Request.GetCompilerVersion()
	if v == nil {
		return "(unknown)"
	}
	out := fmt.Sprintf("v%d.%d.%d", v.GetMajor(), v.GetMinor(), v.GetPatch())
	if s := v.GetSuffix(); s != "" {
		out += "-" + s
	}
	return out
}

func preamble(gen *protogen.Plugin, file *protogen.File, g *protogen.GeneratedFile) {
	g.P("// Code generated by protoc-gen-go-rerpc. DO NOT EDIT.")
	g.P("// versions:")
	g.P("// - protoc-gen-go-rerpc v", rerpc.Version)
	g.P("// - protoc              ", protocVersion(gen))
	if file.Proto.GetOptions().GetDeprecated() {
		comment(g, file.Desc.Path(), " is a deprecated file.")
	} else {
		g.P("// source: ", file.Desc.Path())
	}
	g.P()
	g.P("package ", file.GoPackageName)
	g.P()
}

func content(file *protogen.File, g *protogen.GeneratedFile) {
	if len(file.Services) == 0 {
		return
	}
	handshake(g)
	for _, svc := range file.Services {
		service(file, g, svc)
	}
}

func handshake(g *protogen.GeneratedFile) {
	comment(g, "This is a compile-time assertion to ensure that this generated file ",
		"and the rerpc package are compatible. If you get a compiler error that this constant ",
		"isn't defined, this code was generated with a version of rerpc newer than the one ",
		"compiled into your binary. You can fix the problem by either regenerating this code ",
		"with an older version of rerpc or updating the rerpc version compiled into your binary.")
	g.P("const _ = ", rerpcPackage.Ident("SupportsCodeGenV0"), " // requires reRPC v0.0.1 or later")
	g.P()
}

type names struct {
	Base string

	SimpleClient       string
	FullClient         string
	ClientConstructor  string
	SimpleClientImpl   string
	FullClientImpl     string
	ClientExposeMethod string

	FullServer                 string
	SimpleServer               string
	UnimplementedServer        string
	FullHandlerConstructor     string
	AdaptiveServerImpl         string
	AdaptiveHandlerConstructor string
}

func newNames(service *protogen.Service) names {
	base := service.GoName
	return names{
		Base: base,

		SimpleClient:       fmt.Sprintf("Simple%sClient", base),
		FullClient:         fmt.Sprintf("Full%sClient", base),
		ClientConstructor:  fmt.Sprintf("New%sClient", base),
		SimpleClientImpl:   fmt.Sprintf("%sClient", base),
		FullClientImpl:     fmt.Sprintf("full%sClient", base),
		ClientExposeMethod: "Full",

		SimpleServer:               fmt.Sprintf("Simple%sServer", base),
		FullServer:                 fmt.Sprintf("Full%sServer", base),
		UnimplementedServer:        fmt.Sprintf("Unimplemented%sServer", base),
		FullHandlerConstructor:     fmt.Sprintf("NewFull%sHandler", base),
		AdaptiveServerImpl:         fmt.Sprintf("pluggable%sServer", base),
		AdaptiveHandlerConstructor: fmt.Sprintf("New%sHandler", base),
	}
}

func service(file *protogen.File, g *protogen.GeneratedFile, service *protogen.Service) {
	names := newNames(service)

	clientInterface(g, service, names, false /* full */)
	clientInterface(g, service, names, true /* full */)
	clientImplementation(g, service, names)

	serverInterface(g, service, names)
	serverConstructor(g, service, names)
	adaptiveServerImplementation(g, service, names)
	adaptiveServerConstructor(g, service, names)
	unimplementedServerImplementation(g, service, names)
}

func clientInterface(g *protogen.GeneratedFile, service *protogen.Service, names names, full bool) {
	var name string
	if full {
		name = names.FullClient
		comment(g, name, " is a client for the ", service.Desc.FullName(), " service. ",
			"It's more complex than ", names.SimpleClient, ", but it gives callers more ",
			"fine-grained control (e.g., sending and receiving headers).")
	} else {
		name = names.SimpleClient
		comment(g, name, " is a client for the ", service.Desc.FullName(),
			" service.")
	}
	if service.Desc.Options().(*descriptorpb.ServiceOptions).GetDeprecated() {
		g.P("//")
		deprecated(g)
	}
	g.Annotate(name, service.Location)
	g.P("type ", name, " interface {")
	for _, method := range service.Methods {
		g.Annotate(name+"."+method.GoName, method.Location)
		g.P(method.Comments.Leading, clientSignature(g, method, false /* named */, full))
	}
	g.P("}")
	g.P()
}

func clientSignature(g *protogen.GeneratedFile, method *protogen.Method, named bool, full bool) string {
	if method.Desc.Options().(*descriptorpb.MethodOptions).GetDeprecated() {
		deprecated(g)
	}
	reqName := "req"
	ctxName := "ctx"
	if !named {
		reqName, ctxName = "", ""
	}
	if method.Desc.IsStreamingClient() && method.Desc.IsStreamingServer() {
		// bidi streaming
		return method.GoName + "(" + ctxName + " " + g.QualifiedGoIdent(contextContext) + ") " +
			"*" + g.QualifiedGoIdent(cstreamPackage.Ident("Bidirectional")) +
			"[" + g.QualifiedGoIdent(method.Input.GoIdent) + ", " + g.QualifiedGoIdent(method.Output.GoIdent) + "]"
	}
	if method.Desc.IsStreamingClient() {
		// client streaming
		return method.GoName + "(" + ctxName + " " + g.QualifiedGoIdent(contextContext) + ") " +
			"*" + g.QualifiedGoIdent(cstreamPackage.Ident("Client")) +
			"[" + g.QualifiedGoIdent(method.Input.GoIdent) + ", " + g.QualifiedGoIdent(method.Output.GoIdent) + "]"
	}
	if method.Desc.IsStreamingServer() {
		// server streaming
		if full {
			return method.GoName + "(" + ctxName + " " + g.QualifiedGoIdent(contextContext) +
				", " + reqName + " *" + g.QualifiedGoIdent(rerpcPackage.Ident("Request")) + "[" +
				g.QualifiedGoIdent(method.Input.GoIdent) + "]) " +
				"(*" + g.QualifiedGoIdent(cstreamPackage.Ident("Server")) +
				"[" + g.QualifiedGoIdent(method.Output.GoIdent) + "]" +
				", error)"
		} else {
			return method.GoName + "(" + ctxName + " " + g.QualifiedGoIdent(contextContext) +
				", " + reqName + " *" + g.QualifiedGoIdent(method.Input.GoIdent) + ") " +
				"(*" + g.QualifiedGoIdent(cstreamPackage.Ident("Server")) +
				"[" + g.QualifiedGoIdent(method.Output.GoIdent) + "]" +
				", error)"
		}
	}
	// unary; symmetric so we can re-use server templating
	return method.GoName + serverSignatureParams(g, method, named, full)
}

func clientImplementation(g *protogen.GeneratedFile, service *protogen.Service, names names) {
	// Client struct.
	clientOption := rerpcPackage.Ident("ClientOption")
	comment(g, names.SimpleClientImpl, " is a client for the ", service.Desc.FullName(), " service.")
	if service.Desc.Options().(*descriptorpb.ServiceOptions).GetDeprecated() {
		g.P("//")
		deprecated(g)
	}
	g.P("type ", names.SimpleClientImpl, " struct {")
	g.P("client ", names.FullClientImpl)
	g.P("}")
	g.P()
	g.P("var _ ", names.SimpleClient, " = (*", names.SimpleClientImpl, ")(nil)")

	// Client constructor.
	comment(g, names.ClientConstructor, " constructs a client for the ", service.Desc.FullName(), " service.")
	g.P("//")
	comment(g, "The URL supplied here should be the base URL for the gRPC server ",
		"(e.g., https://api.acme.com or https://acme.com/grpc).")
	if service.Desc.Options().(*descriptorpb.ServiceOptions).GetDeprecated() {
		g.P("//")
		deprecated(g)
	}
	g.P("func ", names.ClientConstructor, " (baseURL string, doer ", rerpcPackage.Ident("Doer"),
		", opts ...", clientOption, ") (*", names.SimpleClientImpl, ", error) {")
	g.P("baseURL = ", stringsPackage.Ident("TrimRight"), `(baseURL, "/")`)
	for _, method := range service.Methods {
		if method.Desc.IsStreamingClient() || method.Desc.IsStreamingServer() {
			g.P(unexport(method.GoName), "Func, err := ", rerpcPackage.Ident("NewClientStream"), "(")
			g.P("doer,")
			if method.Desc.IsStreamingClient() && method.Desc.IsStreamingServer() {
				g.P(rerpcPackage.Ident("StreamTypeBidirectional"), ",")
			} else if method.Desc.IsStreamingClient() {
				g.P(rerpcPackage.Ident("StreamTypeClient"), ",")
			} else {
				g.P(rerpcPackage.Ident("StreamTypeServer"), ",")
			}
			g.P("baseURL,")
			g.P(`"`, service.Desc.ParentFile().Package(), `", // protobuf package`)
			g.P(`"`, service.Desc.Name(), `", // protobuf service`)
			g.P(`"`, method.Desc.Name(), `", // protobuf method`)
			g.P("opts...,")
			g.P(")")
		} else {
			g.P(unexport(method.GoName), "Func, err := ", rerpcPackage.Ident("NewClientFunc"), "[", method.Input.GoIdent, ", ", method.Output.GoIdent, "](")
			g.P("doer,")
			g.P("baseURL,")
			g.P(`"`, service.Desc.ParentFile().Package(), `", // protobuf package`)
			g.P(`"`, service.Desc.Name(), `", // protobuf service`)
			g.P(`"`, method.Desc.Name(), `", // protobuf method`)
			g.P("opts...,")
			g.P(")")
		}
		g.P("if err != nil {")
		g.P("return nil, err")
		g.P("}")
	}
	g.P("return &", names.SimpleClientImpl, "{client: ", names.FullClientImpl, "{")
	for _, method := range service.Methods {
		g.P(unexport(method.GoName), ": ", unexport(method.GoName), "Func,")
	}
	g.P("}}, nil")
	g.P("}")
	g.P()
	var hasFullMethod bool
	for _, method := range service.Methods {
		if method.GoName == names.ClientExposeMethod {
			hasFullMethod = true
		}
		clientMethod(g, service, method, names, false /* full */)
	}
	g.P()
	exposeMethod := names.ClientExposeMethod
	if hasFullMethod {
		exposeMethod += "_"
	}
	comment(g, exposeMethod, " exposes the underlying generic client. Use it if you need",
		" finer control (e.g., sending and receiving headers).")
	if hasFullMethod {
		g.P("//")
		comment(g, "Because there's a \"", names.ClientExposeMethod,
			"\" method defined on this service, this function has an awkward name.")
	} else {
		g.P("func (c *", names.SimpleClientImpl, ") Full() ", names.FullClient, "{")
		g.P("return &c.client")
		g.P("}")
	}
	g.P()

	g.P("type ", names.FullClientImpl, " struct {")
	for _, method := range service.Methods {
		if method.Desc.IsStreamingServer() || method.Desc.IsStreamingClient() {
			g.P(unexport(method.GoName), " ", rerpcPackage.Ident("StreamFunc"))
		} else {
			g.P(unexport(method.GoName), " func", serverSignatureParams(g, method, false /* named */, true /* full */))
		}
	}
	g.P("}")
	g.P()
	g.P("var _ ", names.FullClient, " = (*", names.FullClientImpl, ")(nil)")
	g.P()
	for _, method := range service.Methods {
		clientMethod(g, service, method, names, true /* full */)
	}
}

func clientMethod(g *protogen.GeneratedFile, service *protogen.Service, method *protogen.Method, names names, full bool) {
	receiver := names.SimpleClientImpl
	if full {
		receiver = names.FullClientImpl
	}
	isStreamingClient := method.Desc.IsStreamingClient()
	isStreamingServer := method.Desc.IsStreamingServer()
	comment(g, method.GoName, " calls ", method.Desc.FullName(), ".")
	if method.Desc.Options().(*descriptorpb.MethodOptions).GetDeprecated() {
		g.P("//")
		deprecated(g)
	}
	g.P("func (c *", receiver, ") ", clientSignature(g, method, true /* named */, full), " {")

	if !full {
		// Simple client delegates to the underlying full client.
		if isStreamingServer && !isStreamingClient {
			// server streaming
			g.P("return c.client.", method.GoName, "(ctx, ", rerpcPackage.Ident("NewRequest"), "(req))")
		} else if isStreamingServer || isStreamingClient {
			// client and bidi streaming
			g.P("return c.client.", method.GoName, "(ctx)")
		} else {
			// unary
			g.P("res, err := c.client.", method.GoName, "(ctx, ", rerpcPackage.Ident("NewRequest"), "(req))")
			g.P("if err != nil {")
			g.P("return nil, err")
			g.P("}")
			g.P("return res.Msg, nil")
		}
		g.P("}")
		g.P()
		return
	}

	if isStreamingClient || isStreamingServer {
		g.P("_, stream := c.", unexport(method.GoName), "(ctx)")
		if !isStreamingClient && isStreamingServer {
			// server streaming, we need to send the request.
			g.P("if err := stream.Send(req.Any()); err != nil {")
			g.P("_ = stream.CloseSend(err)")
			g.P("_ = stream.CloseReceive()")
			g.P("return nil, err")
			g.P("}")
			g.P("if err := stream.CloseSend(nil); err != nil {")
			g.P("_ = stream.CloseReceive()")
			g.P("return nil, err")
			g.P("}")
			g.P("return ", cstreamPackage.Ident("NewServer"), "[", method.Output.GoIdent, "]", "(stream), nil")
		} else if isStreamingClient && !isStreamingServer {
			// client streaming
			g.P("return ", cstreamPackage.Ident("NewClient"),
				"[", method.Input.GoIdent, ", ", method.Output.GoIdent, "]", "(stream)")
		} else {
			// bidi streaming
			g.P("return ", cstreamPackage.Ident("NewBidirectional"),
				"[", method.Input.GoIdent, ", ", method.Output.GoIdent, "]", "(stream)")
		}
	} else {
		g.P("return c.", unexport(method.GoName), "(ctx, req)")
	}
	g.P("}")
	g.P()
}

func serverInterface(g *protogen.GeneratedFile, service *protogen.Service, names names) {
	comment(g, names.FullServer, " is a server for the ", service.Desc.FullName(), " service.")
	if service.Desc.Options().(*descriptorpb.ServiceOptions).GetDeprecated() {
		g.P("//")
		deprecated(g)
	}
	g.Annotate(names.FullServer, service.Location)
	g.P("type ", names.FullServer, " interface {")
	for _, method := range service.Methods {
		g.Annotate(names.FullServer+"."+method.GoName, method.Location)
		g.P(method.Comments.Leading, serverSignature(g, method, true /* full */))
	}
	g.P("}")
	g.P()

	comment(g, names.SimpleServer, " is a server for the ", service.Desc.FullName(),
		" service. It's a simpler interface than ", names.FullServer,
		" but doesn't provide header access.")
	if service.Desc.Options().(*descriptorpb.ServiceOptions).GetDeprecated() {
		g.P("//")
		deprecated(g)
	}
	g.Annotate(names.SimpleServer, service.Location)
	g.P("type ", names.SimpleServer, " interface {")
	for _, method := range service.Methods {
		g.Annotate(names.SimpleServer+"."+method.GoName, method.Location)
		g.P(method.Comments.Leading, serverSignature(g, method, false /* full */))
	}
	g.P("}")
	g.P()
}

func serverSignature(g *protogen.GeneratedFile, method *protogen.Method, full bool) string {
	if method.Desc.Options().(*descriptorpb.MethodOptions).GetDeprecated() {
		deprecated(g)
	}
	return method.GoName + serverSignatureParams(g, method, false /* named */, full)
}

func serverSignatureParams(g *protogen.GeneratedFile, method *protogen.Method, named bool, full bool) string {
	ctxName := "ctx "
	reqName := "req "
	streamName := "stream "
	if !named {
		ctxName, reqName, streamName = "", "", ""
	}
	if method.Desc.IsStreamingClient() && method.Desc.IsStreamingServer() {
		// bidi streaming
		return "(" + ctxName + g.QualifiedGoIdent(contextContext) + ", " +
			streamName + "*" + g.QualifiedGoIdent(hstreamPackage.Ident("Bidirectional")) +
			"[" + g.QualifiedGoIdent(method.Input.GoIdent) + ", " + g.QualifiedGoIdent(method.Output.GoIdent) + "]" +
			") error"
	}
	if method.Desc.IsStreamingClient() {
		// client streaming
		return "(" + ctxName + g.QualifiedGoIdent(contextContext) + ", " +
			streamName + "*" + g.QualifiedGoIdent(hstreamPackage.Ident("Client")) +
			"[" + g.QualifiedGoIdent(method.Input.GoIdent) + ", " + g.QualifiedGoIdent(method.Output.GoIdent) + "]" +
			") error"
	}
	if method.Desc.IsStreamingServer() {
		// server streaming
		if full {
			return "(" + ctxName + g.QualifiedGoIdent(contextContext) +
				", " + reqName + "*" + g.QualifiedGoIdent(rerpcPackage.Ident("Request")) + "[" +
				g.QualifiedGoIdent(method.Input.GoIdent) + "], " +
				streamName + "*" + g.QualifiedGoIdent(hstreamPackage.Ident("Server")) +
				"[" + g.QualifiedGoIdent(method.Output.GoIdent) + "]" +
				") error"
		} else {
			return "(" + ctxName + g.QualifiedGoIdent(contextContext) +
				", " + reqName + "*" + g.QualifiedGoIdent(method.Input.GoIdent) +
				", " + streamName + "*" + g.QualifiedGoIdent(hstreamPackage.Ident("Server")) +
				"[" + g.QualifiedGoIdent(method.Output.GoIdent) + "]" +
				") error"
		}
	}
	// unary
	if full {
		return "(" + ctxName + g.QualifiedGoIdent(contextContext) +
			", " + reqName + "*" + g.QualifiedGoIdent(rerpcPackage.Ident("Request")) + "[" +
			g.QualifiedGoIdent(method.Input.GoIdent) + "]) " +
			"(*" + g.QualifiedGoIdent(rerpcPackage.Ident("Response")) + "[" +
			g.QualifiedGoIdent(method.Output.GoIdent) + "], error)"
	}
	return "(" + ctxName + g.QualifiedGoIdent(contextContext) +
		", " + reqName + "*" + g.QualifiedGoIdent(method.Input.GoIdent) + ") " +
		"(*" + g.QualifiedGoIdent(method.Output.GoIdent) + ", error)"
}

func serverConstructor(g *protogen.GeneratedFile, service *protogen.Service, names names) {
	comment(g, names.FullHandlerConstructor, " wraps each method on the service implementation",
		" in a rerpc.Handler. The returned slice can be passed to rerpc.NewServeMux.")
	if service.Desc.Options().(*descriptorpb.ServiceOptions).GetDeprecated() {
		g.P("//")
		deprecated(g)
	}
	g.P("func ", names.FullHandlerConstructor, "(svc ", names.FullServer, ", opts ...", rerpcPackage.Ident("HandlerOption"),
		") []", rerpcPackage.Ident("Handler"), " {")
	g.P("handlers := make([]", rerpcPackage.Ident("Handler"), ", 0, ", len(service.Methods), ")")
	g.P()
	for _, method := range service.Methods {
		hname := unexport(string(method.Desc.Name()))

		if method.Desc.IsStreamingServer() || method.Desc.IsStreamingClient() {
			g.P(hname, " := ", rerpcPackage.Ident("NewStreamingHandler"), "(")
			if method.Desc.IsStreamingServer() && method.Desc.IsStreamingClient() {
				g.P(rerpcPackage.Ident("StreamTypeBidirectional"), ",")
			} else if method.Desc.IsStreamingServer() {
				g.P(rerpcPackage.Ident("StreamTypeServer"), ",")
			} else {
				g.P(rerpcPackage.Ident("StreamTypeClient"), ",")
			}
			g.P(`"`, service.Desc.ParentFile().Package(), `", // protobuf package`)
			g.P(`"`, service.Desc.Name(), `", // protobuf service`)
			g.P(`"`, method.Desc.Name(), `", // protobuf method`)
			g.P("func(ctx ", contextContext, ", stream ", rerpcPackage.Ident("Stream"), ") {")
			if method.Desc.IsStreamingServer() && method.Desc.IsStreamingClient() {
				// bidi streaming
				g.P("typed := ", hstreamPackage.Ident("NewBidirectional"),
					"[", method.Input.GoIdent, ", ", method.Output.GoIdent, "]", "(stream)")
			} else if method.Desc.IsStreamingClient() {
				// client streaming
				g.P("typed := ", hstreamPackage.Ident("NewClient"),
					"[", method.Input.GoIdent, ", ", method.Output.GoIdent, "]", "(stream)")
			} else {
				// server streaming
				g.P("typed := ", hstreamPackage.Ident("NewServer"),
					"[", method.Output.GoIdent, "]", "(stream)")
			}
			if method.Desc.IsStreamingServer() && !method.Desc.IsStreamingClient() {
				g.P("req, err := ", rerpcPackage.Ident("ReceiveRequest"), "[", method.Input.GoIdent, "]",
					"(stream)")
				g.P("if err != nil {")
				g.P("_ = stream.CloseReceive()")
				g.P("_ = stream.CloseSend(err)")
				g.P("return")
				g.P("}")
				g.P("if err = stream.CloseReceive(); err != nil {")
				g.P("_ = stream.CloseSend(err)")
				g.P("return")
				g.P("}")
				g.P("err = svc.", method.GoName, "(ctx, req, typed)")
			} else {
				g.P("err := svc.", method.GoName, "(ctx, typed)")
				g.P("_ = stream.CloseReceive()")
			}
			g.P("if err != nil {")
			// TODO: Dry up context error handling
			g.P("if _, ok := ", rerpcPackage.Ident("AsError"), "(err); !ok {")
			g.P("if ", errorsIs, "(err, ", contextCanceled, ") {")
			g.P("err = ", rerpcPackage.Ident("Wrap"), "(", rerpcPackage.Ident("CodeCanceled"), ", err)")
			g.P("}")
			g.P("if ", errorsIs, "(err, ", contextDeadlineExceeded, ") {")
			g.P("err = ", rerpcPackage.Ident("Wrap"), "(", rerpcPackage.Ident("CodeDeadlineExceeded"), ", err)")
			g.P("}")
			g.P("}")
			g.P("}")
			g.P("_ = stream.CloseSend(err)")
			g.P("},")
			g.P("opts...,")
			g.P(")")
		} else {
			g.P(hname, " := ", rerpcPackage.Ident("NewUnaryHandler"), "(")
			g.P(`"`, service.Desc.ParentFile().Package(), `", // protobuf package`)
			g.P(`"`, service.Desc.Name(), `", // protobuf service`)
			g.P(`"`, method.Desc.Name(), `", // protobuf method`)
			g.P("svc.", method.GoName, ",")
			g.P("opts...,")
			g.P(")")
		}
		g.P("handlers = append(handlers, *", hname, ")")
		g.P()
	}
	g.P("return handlers")
	g.P("}")
	g.P()
}

func unimplementedServerImplementation(g *protogen.GeneratedFile, service *protogen.Service, names names) {
	g.P("var _ ", names.FullServer, " = (*", names.UnimplementedServer, ")(nil) // verify interface implementation")
	g.P()
	comment(g, names.UnimplementedServer, " returns CodeUnimplemented from all methods.")
	g.P("type ", names.UnimplementedServer, " struct {}")
	g.P()
	for _, method := range service.Methods {
		g.P("func (", names.UnimplementedServer, ") ", serverSignature(g, method, true /* full */), "{")
		if method.Desc.IsStreamingServer() || method.Desc.IsStreamingClient() {
			g.P("return ", rerpcPackage.Ident("Errorf"), "(", rerpcPackage.Ident("CodeUnimplemented"), `, "`, method.Desc.FullName(), ` isn't implemented")`)
		} else {
			g.P("return nil, ", rerpcPackage.Ident("Errorf"), "(", rerpcPackage.Ident("CodeUnimplemented"), `, "`, method.Desc.FullName(), ` isn't implemented")`)
		}
		g.P("}")
		g.P()
	}
	g.P()
}

func adaptiveServerImplementation(g *protogen.GeneratedFile, service *protogen.Service, names names) {
	g.P("type ", names.AdaptiveServerImpl, " struct {")
	for _, method := range service.Methods {
		g.P(unexport(method.GoName), " func", serverSignatureParams(g, method, false /* named */, true /* full */))
	}
	g.P("}")
	g.P()
	for _, method := range service.Methods {
		g.P("func (s *", names.AdaptiveServerImpl, ") ", method.GoName,
			serverSignatureParams(g, method, true /* named */, true /* full */), "{")
		if method.Desc.IsStreamingClient() {
			// client and bidi streaming
			g.P("return s.", unexport(method.GoName), "(ctx, stream)")
		} else if method.Desc.IsStreamingServer() {
			// server streaming
			g.P("return s.", unexport(method.GoName), "(ctx, req, stream)")
		} else {
			// unary
			g.P("return s.", unexport(method.GoName), "(ctx, req)")
		}
		g.P("}")
		g.P()
	}
	g.P()
}

func adaptiveServerConstructor(g *protogen.GeneratedFile, service *protogen.Service, names names) {
	comment(g, names.AdaptiveHandlerConstructor, " wraps each method on the service implementation",
		" in a rerpc.Handler. The returned slice can be passed to rerpc.NewServeMux.")
	g.P("//")
	comment(g, "Unlike ", names.FullHandlerConstructor,
		", it allows the service to mix and match the signatures of ",
		names.FullServer, " and ", names.SimpleServer,
		". For each method, it first tries to find a ", names.SimpleServer,
		"-style implementation. If a simple implementation isn't ",
		"available, it falls back to the more complex ", names.FullServer,
		"-style implementation. If neither is available, it returns an error.")
	g.P("//")
	comment(g, "Taken together, this approach lets implementations embed ",
		names.UnimplementedServer, " and implement each method using whichever signature ",
		"is most convenient.")
	if service.Desc.Options().(*descriptorpb.ServiceOptions).GetDeprecated() {
		g.P("//")
		deprecated(g)
	}
	g.P("func ", names.AdaptiveHandlerConstructor, "(svc any, opts ...", rerpcPackage.Ident("HandlerOption"),
		") ([]", rerpcPackage.Ident("Handler"), ", error) {")
	g.P("var impl ", names.AdaptiveServerImpl)
	g.P()
	for _, method := range service.Methods {
		fnamer := unexport(method.GoName) + "er"
		comment(g, "Find an implementation of ", method.Desc.Name())
		if method.Desc.IsStreamingClient() {
			// client and bidi streaming: no simpler signature available, so we just
			// look for the full version.
			g.P("if ", fnamer, ", ok := svc.(interface{", serverSignature(g, method, false /* full */), "}); ok {")
			g.P("impl.", unexport(method.GoName), " = ", fnamer, ".", method.GoName)
			g.P("} else {")
			g.P("return nil, ", errorsPackage.Ident("New"), `("no `, method.GoName, ` implementation found")`)
			g.P("}")
			g.P()
			continue
		}
		g.P("if ", fnamer, ", ok := svc.(interface{", serverSignature(g, method, false /* full */), "}); ok {")
		if method.Desc.IsStreamingServer() {
			// server streaming
			g.P("impl.", unexport(method.GoName), " = func",
				serverSignatureParams(g, method, true /* named */, true /* full */), " {")
			g.P("return ", fnamer, ".", method.GoName, "(ctx, req.Msg, stream)")
			g.P("}")
		} else {
			// unary
			g.P("impl.", unexport(method.GoName), " = func",
				serverSignatureParams(g, method, true /* named */, true /* full */), " {")
			g.P("res, err := ", fnamer, ".", method.GoName, "(ctx, req.Msg)")
			g.P("if err != nil {")
			g.P("return nil, err")
			g.P("}")
			g.P("return ", rerpcPackage.Ident("NewResponse"), "(res), nil")
			g.P("}")
		}
		g.P("} else if ", fnamer, ", ok := svc.(interface{", serverSignature(g, method, true /* full */), "}); ok {")
		g.P("impl.", unexport(method.GoName), " = ", fnamer, ".", method.GoName)
		g.P("} else {")
		g.P("return nil, ", errorsPackage.Ident("New"), `("no `, method.GoName, ` implementation found")`)
		g.P("}")
		g.P()
	}
	g.P("return ", names.FullHandlerConstructor, "(&impl, opts...), nil")
	g.P("}")
	g.P()
}

func unexport(s string) string { return strings.ToLower(s[:1]) + s[1:] }