package sniff

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing/common/buf"
	E "github.com/sagernet/sing/common/exceptions"
)

type SniffData struct {
	metadata *adapter.InboundContext
	err      error
}

type (
	StreamSniffer = func(ctx context.Context, reader io.Reader, sniffdata chan SniffData, wg *sync.WaitGroup)
	PacketSniffer = func(ctx context.Context, packet []byte, sniffdata chan SniffData, wg *sync.WaitGroup)
)

func PeekStream(ctx context.Context, conn net.Conn, buffer *buf.Buffer, timeout time.Duration, sniffers ...StreamSniffer) (*adapter.InboundContext, error) {
	if timeout == 0 {
		timeout = C.ReadPayloadTimeout
	}
	deadline := time.Now().Add(timeout)
	var errors []error
	for i := 0; i < 3; i++ {
		err := conn.SetReadDeadline(deadline)
		if err != nil {
			return nil, E.Cause(err, "set read deadline")
		}
		_, err = buffer.ReadOnceFrom(conn)
		err = E.Errors(err, conn.SetReadDeadline(time.Time{}))
		if err != nil {
			if i > 0 {
				break
			}
			return nil, E.Cause(err, "read payload")
		}
		sniffdatas := make(chan SniffData, len(sniffers))
		var wg sync.WaitGroup
		for _, sniffer := range sniffers {
			wg.Add(1)
			go sniffer(ctx, bytes.NewReader(buffer.Bytes()), sniffdatas, &wg)
		}
		defer func(wg *sync.WaitGroup, sniffdatas chan SniffData) {
			go func(wg *sync.WaitGroup, sniffdatas chan SniffData) {
				wg.Wait()
				close(sniffdatas)
			}(wg, sniffdatas)
		}(&wg, sniffdatas)
		for i := 0; i < len(sniffers); i++ {
			data := <-sniffdatas
			if data.metadata != nil {
				return data.metadata, nil
			}
			if data.err != nil {
				errors = append(errors, data.err)
			}
		}
	}
	return nil, E.Errors(errors...)
}

func PeekPacket(ctx context.Context, packet []byte, sniffers ...PacketSniffer) (*adapter.InboundContext, error) {
	var errors []error
	sniffdatas := make(chan SniffData, len(sniffers))
	var wg sync.WaitGroup
	for _, sniffer := range sniffers {
		wg.Add(1)
		go sniffer(ctx, packet, sniffdatas, &wg)
	}
	defer func(wg *sync.WaitGroup, sniffdatas chan SniffData) {
		go func(wg *sync.WaitGroup, sniffdatas chan SniffData) {
			wg.Wait()
			close(sniffdatas)
		}(wg, sniffdatas)
	}(&wg, sniffdatas)
	for i := 0; i < len(sniffers); i++ {
		data := <-sniffdatas
		if data.metadata != nil {
			return data.metadata, nil
		}
		if data.err != nil {
			errors = append(errors, data.err)
		}
	}
	return nil, E.Errors(errors...)
}

func (d *SniffData) GetMetadata() adapter.InboundContext {
	return *d.metadata
}

func (d *SniffData) GetErr() error {
	return d.err
}
