package main

import (
	"context"
	"fmt"
	"log"
	"time"
)

type mlogInfo struct {
	addr            string
	refreshTime     time.Time
	parent          *subscribeLog
	waitForResponse bool
}

func (i *mlogInfo) Init(args ...interface{}) error {
	if len(args) != 2 {
		log.Printf("[E]args(conn *net.UDPConn, processor IProcessor, parent *udpEndpoint) is needed\n")
		return fmt.Errorf("args(conn *net.UDPConn, processor IProcessor, parent *udpEndpoint) is needed")
	}
	log.Printf("......args=%#v\n", args)
	if addr, ok := args[0].(string); !ok || addr == "" {
		log.Printf("[E]args[0](%#v) must be a valid addr\n", args[0])
		return fmt.Errorf("args[0](%#v) must be a valid addr", args[0])
	} else {
		if parent, ok := args[1].(*subscribeLog); !ok || parent == nil {
			log.Printf("[E]args[1](%#v) must be a valid subscribeLog pointer\n", args[1])
			return fmt.Errorf("args[1](%#v) must be a valid subscribeLog pointer", args[1])
		} else {
			i.addr = addr
			i.parent = parent
			i.refreshTime = time.Now()
		}
	}
	return nil
}

func (i *mlogInfo) RunOnce(ctx context.Context) error {
	now := time.Now()
	if i.refreshTime.Add(5 * time.Second).Before(now) {
		if i.waitForResponse {
			log.Printf("[E]%s no response\n", i.addr)
			return fmt.Errorf("%s no response", i.addr)
		} else {
			err := i.parent.sendHeartbeat(i.addr)
			if err != nil {
				log.Printf("[E]sent heartbeat to(%s) failed:%v\n", i.addr, err)
				return err
			} else {
				log.Printf("[I]sent heartbeat to(%s) success\n", i.addr)
				i.refreshTime = now
				i.waitForResponse = true
			}
		}
	}
	return nil
}
func (i *mlogInfo) Destroy() {

}

func (i *mlogInfo) UserData() interface{} {
	return i
}
