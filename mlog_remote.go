package mlog

import (
	"fmt"
	"os"
	"path"
)

type remoteWriter struct {
	writer
	Addr     string
	Hostname string
	Facility string // defaults to current process name
	Proto    string
}

var mRemoteWriter *remoteWriter = nil

// @title       SetRemoteAddr
// @description
// @auth        ranwei      2021/4/8   14:36
// @param       proto       string         ", tcp or udp"
// @param       addr        string         "ip:port"
// @return
func SetRemoteAddr(proto, addr string) {
	if proto != "tcp" && proto != "udp" {
		panic(fmt.Errorf("invalid proto=%v", proto))
	}
	var err error
	mRemoteWriter = &remoteWriter{}
	if proto == "tcp" {
		mRemoteWriter.writer, err = newTCPWriter(addr)
		if err != nil {
			panic(fmt.Errorf("newTCPWriter error:%v", err))
		}
	} else if proto == "udp" {
		mRemoteWriter.writer, err = newUDPWriter(addr)
		if err != nil {
			panic(fmt.Errorf("newTCPWriter error:%v", err))
		}
	}
	if mRemoteWriter.Hostname, err = os.Hostname(); err != nil {
		mRemoteWriter.writer.close()
		panic(fmt.Errorf("can't get hostname:%v", err))
	}
	mRemoteWriter.Facility = path.Base(os.Args[0])
	mRemoteWriter.Proto = proto
	mRemoteWriter.Addr = addr
}
