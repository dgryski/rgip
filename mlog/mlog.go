package mlog

import (
	"flag"
	"fmt"
	stdlog "log"
	"log/syslog"
	"os"
	"path"
)

// Overwrite CLI setting and log to stdout
func LogToStdout() {
	*logToStdout = true
}

// Println logs to syslog, and optionally stdout, with behaviour like fmt.Println
func Println(a ...interface{}) { writeLog(fmt.Sprintln(a...)) }

// Printf logs to syslog, and optionally stdout, with behaviour like fmt.Printf
func Printf(format string, a ...interface{}) { writeLog(fmt.Sprintf(format, a...)) }

func writeLog(s string) {

	err := lsyslog.Info(s)
	if err != nil {
		lstdout.Printf("tell: error writing to syslog: %v", err)
	}

	if *logToStdout {
		// calling 'Output' directly to avoid an extra internal call to fmt.Sprint()
		lstdout.Output(2, s)
	}
}

// Fatal is equivalent to Println, followed by os.Exit(1)
func Fatal(a ...interface{}) {
	Println(a...)
	os.Exit(1)
}

// Fatalf is equivalent to Printf, followed by os.Exit(1)
func Fatalf(format string, a ...interface{}) {
	Printf(format, a...)
	os.Exit(1)
}

var logToStdout = flag.Bool("logtostdout", false, "log to stdout")

var lstdout *stdlog.Logger

var lsyslog *syslog.Writer

func init() {
	lstdout = stdlog.New(os.Stdout, "", stdlog.LstdFlags)
	arg0 := path.Base(os.Args[0])
	lsyslog, _ = syslog.New(syslog.LOG_INFO|syslog.LOG_DAEMON, arg0)
}
