package binlog

import (
	"encoding/binary"
	"os"
	"testing"

	"github.com/youtube/vitess/go/sync2"
	"github.com/youtube/vitess/go/testfiles"
	"github.com/youtube/vitess/go/vt/binlog/proto"
	"github.com/youtube/vitess/go/vt/mysqlctl"
	myproto "github.com/youtube/vitess/go/vt/mysqlctl/proto"
)

func BenchmarkFileStreamerParseEvents(b *testing.B) {
	filename := testfiles.Locate("binlog_test/vt-0000062347-bin.000001")
	var svm sync2.ServiceManager
	count := 0
	bls := newTestBinlogFileStreamer("vt_test_database", "", myproto.ReplicationPosition{}, func(tx *proto.BinlogTransaction) error {
		count++
		return nil
	})

	for i := 0; i < b.N; i++ {
		if err := bls.file.Init(filename, 0); err != nil {
			b.Fatalf("%v", err)
		}
		svm.Go(bls.run)
		if err := svm.Join(); err != nil {
			b.Errorf("%v", err)
		}
		bls.file.Close()
	}

	b.Logf("%d transactions processed", count)
}

func readEvents(b *testing.B, filename string) <-chan proto.BinlogEvent {
	events := make(chan proto.BinlogEvent)
	go func() {
		defer close(events)

		file, err := os.Open(filename)
		if err != nil {
			b.Fatalf("can't open file %s: %v", filename, err)
		}
		defer file.Close()

		// skip binlog magic header
		file.Seek(4, os.SEEK_SET)

		for {
			// read event header
			header := make([]byte, 19)
			if _, err := file.Read(header); err != nil {
				return
			}
			// get total event size
			size := binary.LittleEndian.Uint32(header[9 : 9+4])
			// read the rest of the event
			buf := make([]byte, size)
			copy(buf[:19], header)
			if _, err := file.Read(buf[19:]); err != nil {
				return
			}
			// convert to a BinlogEvent
			events <- mysqlctl.NewGoogleBinlogEvent(buf)
		}
	}()
	return events
}

func BenchmarkConnStreamerParseEvents(b *testing.B) {
	filename := testfiles.Locate("binlog_test/vt-0000062347-bin.000001")
	var svm sync2.ServiceManager
	count := 0
	bls := &binlogConnStreamer{dbname: "vt_test_database", sendTransaction: func(tx *proto.BinlogTransaction) error {
		count++
		return nil
	}}

	for i := 0; i < b.N; i++ {
		events := readEvents(b, filename)
		svm.Go(func(svc *sync2.ServiceContext) error {
			return bls.parseEvents(svc, events)
		})
		if err := svm.Join(); err != ServerEOF {
			b.Errorf("%v", err)
		}
	}

	b.Logf("%d transactions processed", count)
}