package platform

import (
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"connectrpc.com/connect"
)

// snakeCaseJSONCodec is the Connect JSON codec used by the platform.
//
// It overrides Connect's default JSON behaviour to emit snake_case
// field names on the wire (the original proto field names), matching
// the gateway's protojson configuration and the Stripe-style aesthetic
// we use elsewhere in the API.
//
// Input still accepts BOTH snake_case and lowerCamelCase per the
// proto3 JSON spec — that's mandated by protojson and is not something
// we need to opt into.
//
// DiscardUnknown lets older servers tolerate new optional fields a
// future client might add without breaking. It's a one-way ratchet on
// schema evolution that we get for free.
type snakeCaseJSONCodec struct {
	marshal   protojson.MarshalOptions
	unmarshal protojson.UnmarshalOptions
}

func newSnakeCaseJSONCodec() *snakeCaseJSONCodec {
	return &snakeCaseJSONCodec{
		marshal: protojson.MarshalOptions{
			UseProtoNames: true,
		},
		unmarshal: protojson.UnmarshalOptions{
			DiscardUnknown: true,
		},
	}
}

// Name returns "json" so registering this codec replaces Connect's
// built-in JSON codec rather than living alongside it.
func (c *snakeCaseJSONCodec) Name() string { return "json" }

func (c *snakeCaseJSONCodec) Marshal(v any) ([]byte, error) {
	pm, ok := v.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("connect json codec: %T does not implement proto.Message", v)
	}
	return c.marshal.Marshal(pm)
}

func (c *snakeCaseJSONCodec) Unmarshal(data []byte, v any) error {
	pm, ok := v.(proto.Message)
	if !ok {
		return fmt.Errorf("connect json codec: %T does not implement proto.Message", v)
	}
	return c.unmarshal.Unmarshal(data, pm)
}

var _ connect.Codec = (*snakeCaseJSONCodec)(nil)
