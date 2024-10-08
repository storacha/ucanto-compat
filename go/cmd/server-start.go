package cmd

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	ipldprime "github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/node/basicnode"
	ipldschema "github.com/ipld/go-ipld-prime/schema"
	"github.com/storacha/go-ucanto/core/invocation"
	"github.com/storacha/go-ucanto/core/ipld"
	"github.com/storacha/go-ucanto/core/receipt"
	"github.com/storacha/go-ucanto/core/schema"
	"github.com/storacha/go-ucanto/principal"
	ed25519 "github.com/storacha/go-ucanto/principal/ed25519/signer"
	"github.com/storacha/go-ucanto/server"
	uhttp "github.com/storacha/go-ucanto/transport/http"
	"github.com/storacha/go-ucanto/ucan"
	"github.com/storacha/go-ucanto/validator"
	"github.com/urfave/cli/v2"
)

type testEchoCaveats struct {
	Echo string
}

func (c testEchoCaveats) Build() (ipld.Node, error) {
	typ, err := testEchoCaveatsType()
	if err != nil {
		return nil, err
	}
	return ipld.WrapWithRecovery(&c, typ)
}

func testEchoCaveatsType() (ipldschema.Type, error) {
	ts, err := ipldprime.LoadSchemaBytes([]byte(`
	  type TestEchoCaveats struct {
		  echo String
		}
	`))
	if err != nil {
		return nil, err
	}
	return ts.TypeByName("TestEchoCaveats"), nil
}

type testEchoSuccess struct {
	Echo string
}

func (ok testEchoSuccess) ToIPLD() (ipld.Node, error) {
	np := basicnode.Prototype.Any
	nb := np.NewBuilder()
	ma, _ := nb.BeginMap(1)
	ma.AssembleKey().AssignString("echo")
	ma.AssembleValue().AssignString(ok.Echo)
	ma.Finish()
	return nb.Build(), nil
}

func createServer(signer principal.Signer) (server.ServerView, error) {
	typ, err := testEchoCaveatsType()
	if err != nil {
		return nil, err
	}

	testecho := validator.NewCapability(
		"test/echo",
		schema.DIDString(),
		schema.Struct[testEchoCaveats](typ, nil),
		validator.DefaultDerives,
	)

	return server.NewServer(
		signer,
		server.WithServiceMethod(
			testecho.Can(),
			server.Provide(
				testecho,
				func(cap ucan.Capability[testEchoCaveats], inv invocation.Invocation, ctx server.InvocationContext) (testEchoSuccess, receipt.Effects, error) {
					return testEchoSuccess{Echo: cap.Nb().Echo}, nil, nil
				},
			),
		),
	)
}

func ServerStart(cCtx *cli.Context) error {
	signer, err := ed25519.Generate()
	if err != nil {
		return err
	}

	server, err := createServer(signer)
	if err != nil {
		return err
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		res, err := server.Request(uhttp.NewHTTPRequest(r.Body, r.Header))
		if err != nil {
			fmt.Printf("error: %+v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		for key, vals := range res.Headers() {
			for _, v := range vals {
				w.Header().Add(key, v)
			}
		}

		if res.Status() != 0 {
			w.WriteHeader(res.Status())
		}

		_, err = io.Copy(w, res.Body())
		if err != nil {
			fmt.Printf("stream error: %s", err)
		}
	})

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return err
	}

	http.HandleFunc("/shutdown", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		go func() {
			time.Sleep(time.Second)
			listener.Close()
		}()
	})

	port := listener.Addr().(*net.TCPAddr).Port
	fmt.Printf("{\"id\":\"%s\",\"url\":\"http://127.0.0.1:%d\"}\n", signer.DID().String(), port)

	http.Serve(listener, nil)
	return nil
}
