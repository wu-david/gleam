// Package netchan creates network channels. The network channels are managed by
// glow agent.
package netchan

import (
	"fmt"
	"io"
	"net"
	"os"
	"sync"

	"github.com/chrislusf/gleam/distributed/cmd"
	"github.com/chrislusf/gleam/util"
	"github.com/golang/protobuf/proto"
)

func DialReadChannel(wg *sync.WaitGroup, readerName string, address string, channelName string, outChan io.WriteCloser) error {

	readWriter, err := net.Dial("tcp", address)
	if err != nil {
		wg.Done()
		return fmt.Errorf("Fail to dial read %s: %v", address, err)
	}
	defer readWriter.Close()

	data, err := proto.Marshal(&cmd.ControlMessage{
		ReadRequest: &cmd.ReadRequest{
			ChannelName: proto.String(channelName),
			ReaderName:  proto.String(readerName),
		},
	})

	util.WriteMessage(readWriter, data)

	util.ReaderToChannel(wg, channelName, readWriter, outChan, true, os.Stderr)

	return nil
}

func DialWriteChannel(wg *sync.WaitGroup, writerName string, address string, channelName string, inChan io.Reader, readerCount int) error {

	readWriter, err := net.Dial("tcp", address)
	if err != nil {
		wg.Done()
		return fmt.Errorf("Fail to dial write %s: %v", address, err)
	}
	defer readWriter.Close()

	data, err := proto.Marshal(&cmd.ControlMessage{
		WriteRequest: &cmd.WriteRequest{
			ChannelName: proto.String(channelName),
			ReaderCount: proto.Int32(int32(readerCount)),
			WriterName:  proto.String(writerName),
		},
	})

	util.WriteMessage(readWriter, data)

	util.ChannelToWriter(wg, channelName, inChan, readWriter, os.Stderr)

	// println("writing to data", channelName, "finished.")

	return nil
}
