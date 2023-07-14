package main

import (
	"time"

	"mlib.com/mlog"
)

func test1() {
	mlog.Info("hello")
}

func main() {
	mlog.SetLogDir("logs")
	for iLoop := 0; iLoop < 1000; iLoop++ {
		if iLoop%4 == 0 {
			mlog.Debugf("hello%d", iLoop)
		} else if iLoop%4 == 1 {
			mlog.Infof("hello%d", iLoop)
		} else if iLoop%4 == 2 {
			mlog.Warningf("hello%d", iLoop)
		} else if iLoop%4 == 3 {
			mlog.Errorf("hello%d", iLoop)
		}
		test1()
		test2()
		time.Sleep(1 * time.Second)
	}
	mlog.Flush()
}
