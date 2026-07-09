package mcp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
)

// Serve processes newline-delimited JSON-RPC 2.0 requests from in, writing
// one compact response per line to out, until EOF (returning nil), a write
// or read failure, or ctx cancellation (returning the ctx error). A nil ctx
// is treated as context.Background.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	reader := bufio.NewReader(in)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		line, err := reader.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}

		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			req, rpcErr := ParseJSONRPCRequest(trimmed)
			if rpcErr != nil {
				if writeErr := writeJSONRPCResponse(out, NewJSONRPCErrorResponse(req.ResponseID(), rpcErr.Code, rpcErr.Message, rpcErr.Data)); writeErr != nil {
					return writeErr
				}
			} else if resp := s.dispatch(req); resp != nil {
				if writeErr := writeJSONRPCResponse(out, *resp); writeErr != nil {
					return writeErr
				}
			}
		}

		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

func writeJSONRPCResponse(out io.Writer, resp JSONRPCResponse) error {
	encoded, err := SerializeJSONRPCResponse(resp)
	if err != nil {
		return err
	}
	if _, err := out.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return nil
}
