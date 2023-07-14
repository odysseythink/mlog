package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"time"

	"mlib.com/mcommu"
	"mlib.com/mcommu/processor"
	"mlib.com/mlog/pbapi"
	"mlib.com/mrun"
)

var (
	defaultLogStartPort = 29999
)

func (s *subscribeLog) PbLogInfoRspHandle(conn mcommu.IConn, req interface{}) {
	subscribe := &pbapi.PK_LOG_SUBSCRIBE_REQ{Name: "mlog", Pwd: "mlog123456"}
	if infoRsp, ok := req.(*pbapi.PK_LOG_INFO_RSP); !ok || infoRsp == nil {
		log.Printf("invalid req=%#v\n", req)
	} else {
		log.Printf("rsp=%#v\n", infoRsp)
		if infoRsp.Errmsg == "" {
			if infoRsp.Facility == s.facility {
				subscribe.Facility = infoRsp.Facility
				conn.Write(subscribe)
				// mi := &mlogInfo{addr: conn.RemoteAddr(), refreshTime: time.Now()}
				// s.mlogAddrs.Store(conn.RemoteAddr(), mi)
			}
		}
	}
}

func (s *subscribeLog) PbLogSubscribeRspHandle(conn mcommu.IConn, req interface{}) {
	if infoRsp, ok := req.(*pbapi.PK_LOG_SUBSCRIBE_RSP); !ok || infoRsp == nil {
		log.Printf("invalid req=%#v\n", req)
	} else {
		log.Printf("rsp=%#v\n", infoRsp)
		if infoRsp.Errmsg == "" {
			exist := false
			s.mlogAddrs.Range(func(m mrun.IModule) bool {
				if m.UserData().(*mlogInfo).addr == conn.RemoteAddr() {
					m.UserData().(*mlogInfo).waitForResponse = false
					m.UserData().(*mlogInfo).refreshTime = time.Now()
					exist = true
					return false
				}
				return true
			})
			if !exist {
				log.Printf("[I]add remote%s\n", conn.RemoteAddr())
				s.mlogAddrs.Register(&mlogInfo{}, []mrun.ModuleMgrOption{mrun.NewModuleErrorOption(s.onError)}, conn.RemoteAddr(), s)
			}
		}
	}
}

func (s *subscribeLog) PbLogPublishNoticeHandle(conn mcommu.IConn, req interface{}) {
	if msg, ok := req.(*pbapi.PK_LOG_PUBLISH_NOTICE); !ok || msg == nil {
		log.Printf("invalid req=%#v\n", req)
	} else {
		fmt.Printf("rsp=%#v\n", msg)
		s.mlogAddrs.Range(func(m mrun.IModule) bool {
			if m.UserData().(*mlogInfo).addr == conn.RemoteAddr() {
				log.Printf("%s refresh\n", conn.RemoteAddr())
				m.UserData().(*mlogInfo).refreshTime = time.Now()
				return false
			}
			return true
		})
	}
}

type subscribeLog struct {
	mlogIP       string
	facility     string
	processor    mcommu.IProcessor
	mlogAddrs    mrun.ModuleMgr
	communicator mcommu.ICommunicator
	checkTimer   *time.Timer
}

func (s *subscribeLog) Init(args ...interface{}) error {
	msgprocessor := &processor.ProtobufProcessor{}
	msgprocessor.RegisterHandler(uint32(pbapi.PK_LOG_INFO_REQ_CMD), &pbapi.PK_LOG_INFO_REQ{}, nil)
	msgprocessor.RegisterHandler(uint32(pbapi.PK_LOG_INFO_RSP_CMD), &pbapi.PK_LOG_INFO_RSP{}, s.PbLogInfoRspHandle)
	msgprocessor.RegisterHandler(uint32(pbapi.PK_LOG_SUBSCRIBE_REQ_CMD), &pbapi.PK_LOG_SUBSCRIBE_REQ{}, nil)
	msgprocessor.RegisterHandler(uint32(pbapi.PK_LOG_SUBSCRIBE_RSP_CMD), &pbapi.PK_LOG_SUBSCRIBE_RSP{}, s.PbLogSubscribeRspHandle)
	msgprocessor.RegisterHandler(uint32(pbapi.PK_LOG_PUBLISH_NOTICE_CMD), &pbapi.PK_LOG_PUBLISH_NOTICE{}, s.PbLogPublishNoticeHandle)
	s.processor = msgprocessor
	err := s.mlogAddrs.Init()
	if err != nil {
		log.Printf("mlogAddrs init failed:%v\n", err)
		return fmt.Errorf("mlogAddrs init failed:%v", err)
	}
	for iLoop := 0; iLoop < 100; iLoop++ {
		port := defaultLogStartPort + rand.Intn(100)
		s.communicator = mcommu.NewCommunicator("udp", "0.0.0.0:"+strconv.Itoa(port), 100, 100, 50, s.processor)
		if s.communicator == nil {
			log.Println("create udp communicator failed, retry")
			continue
		} else {
			break
		}
	}
	if s.communicator == nil {
		log.Println("can't create udp communicator")
		return fmt.Errorf(("can't create udp communicator"))
	}
	s.broadcast()
	s.checkTimer = time.NewTimer(1 * time.Second)

	return nil
}

func (s *subscribeLog) RunOnce(ctx context.Context) error {
	select {
	case <-s.checkTimer.C:
		s.broadcast()
		s.checkTimer.Reset(1 * time.Second)
	default:
		return nil
	}
	return nil
}
func (s *subscribeLog) Destroy() {
	if s.communicator != nil {
		s.communicator.Close()
	}
}

func (s *subscribeLog) broadcast() {
	for iLoop := 0; iLoop < 100; iLoop++ {
		addr := s.mlogIP + ":" + strconv.Itoa(19999+iLoop)
		exist := false
		s.mlogAddrs.Range(func(m mrun.IModule) bool {
			if m.UserData().(*mlogInfo).addr == addr {
				exist = true
				return false
			}
			return true
		})
		if !exist {
			s.communicator.SendToRemote(addr, &pbapi.PK_LOG_INFO_REQ{Name: "mlog", Pwd: "mlog123456"})
		}
	}
}

func (s *subscribeLog) sendHeartbeat(addr string) error {
	return s.communicator.SendToRemote(addr, &pbapi.PK_LOG_SUBSCRIBE_REQ{Name: "mlog", Pwd: "mlog123456", Facility: s.facility})
}

func (s *subscribeLog) UserData() interface{} {
	return nil
}

func (s *subscribeLog) onError(m mrun.IModule, err error) {
	s.broadcast()
}

func main() {
	log.SetFlags(log.Lmicroseconds | log.Lshortfile)
	slog := &subscribeLog{}
	flag.StringVar(&slog.facility, "facility", "", "define the facility of mlog wanted to monitor")
	flag.StringVar(&slog.mlogIP, "ip", "", "define the ip of mlog wanted to monitor")
	flag.Parse()
	if slog.facility == "" || slog.mlogIP == "" {
		log.Printf("[W]usage: mlog_subscribe --facility=test --ip=192.168.1.111")
		return
	}

	mrun.Run(slog)
}
