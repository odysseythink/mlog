package mlog

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path"
	"strconv"
	"sync"
	"time"

	"mlib.com/mcommu"
	"mlib.com/mcommu/processor"
	"mlib.com/mlog/pbapi"
)

var (
	defaultLogStartPort = 19999
)

func newRemoteLogger() *remoteLogger {
	var err error
	l := &remoteLogger{}
	if l.Hostname, err = os.Hostname(); err != nil {
		log.Printf("can't get hostname:%v\n", err)
		return nil
	}
	l.Facility = path.Base(os.Args[0])
	l.Init()
	return l
}

type remoteLogger struct {
	Addr           string
	Hostname       string
	Facility       string // defaults to current process name
	polling        chan *pbapi.PK_LOG_PUBLISH_NOTICE
	subscribeAddr  sync.Map
	communicator   mcommu.ICommunicator
	processor      mcommu.IProcessor
	ctx            context.Context
	wg             sync.WaitGroup
	ctxCancelFunc  context.CancelFunc
	publishMsgPool sync.Pool
}

func (w *remoteLogger) PbLogInfoReqHandle(conn mcommu.IConn, req interface{}) {
	rsp := &pbapi.PK_LOG_INFO_RSP{}
	if infoReq, ok := req.(*pbapi.PK_LOG_INFO_REQ); !ok || infoReq == nil {
		log.Printf("invalid req=%#v\n", req)
		rsp.Errmsg = "invalid req type"
	} else {
		// log.Printf("rsp=%#v\n", helloRsp)
		if infoReq.Name != "mlog" && infoReq.Pwd != "mlog123456" {
			rsp.Errmsg = "auth failed"
		} else {
			rsp.Facility = w.Facility
		}
	}
	conn.Write(rsp)
}

func (w *remoteLogger) PbLogSubscribeReqHandle(conn mcommu.IConn, req interface{}) {
	rsp := &pbapi.PK_LOG_SUBSCRIBE_RSP{}
	if subscribeReq, ok := req.(*pbapi.PK_LOG_SUBSCRIBE_REQ); !ok || subscribeReq == nil {
		log.Printf("invalid req=%#v\n", req)
		rsp.Errmsg = "invalid req type"
	} else {
		// log.Printf("rsp=%#v\n", helloRsp)
		if subscribeReq.Name == "mlog" && subscribeReq.Pwd == "mlog123456" && subscribeReq.Facility == w.Facility {
			w.subscribeAddr.Store(conn.RemoteAddr(), struct{}{})
			if w.Addr == "" {
				w.Addr = subscribeReq.LogAddr
			}
		} else {
			rsp.Errmsg = "auth failed"
		}
	}
	conn.Write(rsp)
}

func (w *remoteLogger) Init() {
	if w.processor == nil {
		msgprocessor := &processor.ProtobufProcessor{}
		msgprocessor.RegisterHandler(uint32(pbapi.PK_LOG_INFO_REQ_CMD), &pbapi.PK_LOG_INFO_REQ{}, w.PbLogInfoReqHandle)
		msgprocessor.RegisterHandler(uint32(pbapi.PK_LOG_INFO_RSP_CMD), &pbapi.PK_LOG_INFO_RSP{}, nil)
		msgprocessor.RegisterHandler(uint32(pbapi.PK_LOG_SUBSCRIBE_REQ_CMD), &pbapi.PK_LOG_SUBSCRIBE_REQ{}, w.PbLogSubscribeReqHandle)
		msgprocessor.RegisterHandler(uint32(pbapi.PK_LOG_SUBSCRIBE_RSP_CMD), &pbapi.PK_LOG_SUBSCRIBE_RSP{}, nil)
		msgprocessor.RegisterHandler(uint32(pbapi.PK_LOG_PUBLISH_NOTICE_CMD), &pbapi.PK_LOG_PUBLISH_NOTICE{}, nil)
		w.processor = msgprocessor
	}
	w.Facility = path.Base(os.Args[0])
	w.ctx, w.ctxCancelFunc = context.WithCancel(context.Background())
	w.polling = make(chan *pbapi.PK_LOG_PUBLISH_NOTICE)
	w.wg.Add(1)

	go func() {
		defer func() {
			w.wg.Done()
		}()
		timer := time.NewTimer(1 * time.Millisecond)
	LOOP:
		for {
			select {
			case <-w.ctx.Done():
				log.Printf("[D]context done\n")
				break LOOP
			case <-timer.C:
				if w.communicator == nil {
					port := defaultLogStartPort + rand.Intn(100)
					w.communicator = mcommu.NewCommunicator("udp", "0.0.0.0:"+strconv.Itoa(port), 50, 1024, 50, w.processor)
					if w.communicator == nil {
						timer.Reset(30 * time.Second)
					}
				}
			case msg := <-w.polling:
				if w.communicator != nil {
					w.subscribeAddr.Range(func(key, value interface{}) bool {
						if err := w.communicator.SendToRemote(key.(string), msg); err != nil {
							log.Printf("send to remote failed:%v\n", err)
							w.subscribeAddr.Delete(key)
						}
						return true
					})
				}
			}
		}
	}()
	//
}

func (w *remoteLogger) Publish(l *loggingT, level int32, pid int, file, funcname string, line int, msgtime time.Time, format string, args ...interface{}) error {
	if w.communicator == nil {
		return fmt.Errorf("no communicator")
	}
	if w.polling == nil {
		// no polling
		return fmt.Errorf("no polling rounting")
	}
	remoteBuf := l.getBuffer()
	if format != "" {
		fmt.Fprintf(remoteBuf, format, args...)
	} else {
		fmt.Fprintln(remoteBuf, args...)
	}
	m := w.constructMessage(remoteBuf.Bytes(), w.Hostname, int32(level), pid, file, funcname, line, w.Facility, msgtime)

	timeout := time.NewTimer(time.Microsecond * 10)

	select {
	case w.polling <- m:
		return nil
	case <-timeout.C:
		return fmt.Errorf("write chan time out")
	case <-w.ctx.Done():
		return fmt.Errorf("polling is done")
	}
}

func (w *remoteLogger) constructMessage(p []byte, hostname string, level int32, pid int, file, funcname string, line int, facility string, msgtime time.Time) (m *pbapi.PK_LOG_PUBLISH_NOTICE) {
	// remove trailing and leading whitespace
	p = bytes.TrimSpace(p)

	// If there are newlines in the message, use the first line
	// for the short message and set the full message to the
	// original input.  If the input has no newlines, stick the
	// whole thing in Short.
	if w.publishMsgPool.New == nil {
		w.publishMsgPool.New = func() interface{} {
			return &pbapi.PK_LOG_PUBLISH_NOTICE{}
		}
	}
	m = w.publishMsgPool.Get().(*pbapi.PK_LOG_PUBLISH_NOTICE)

	m.Host = hostname
	m.Msg = string(p)
	m.Timestamp = msgtime.Format("2006-01-02 15:04:05.000000")
	m.Level = level
	m.Pid = int32(pid)
	m.File = file
	m.Funcname = funcname
	m.Line = int32(line)
	m.Facility = facility

	return m
}

func (w *remoteLogger) Destroy() {
	if w.ctxCancelFunc != nil {
		w.ctxCancelFunc()
	}
	if w.communicator != nil {
		w.communicator.Close()
	}
	w.wg.Wait()
}

var mRemoteWriter *remoteLogger = newRemoteLogger()
