package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

// Windows keeps all of this in the System and Application event logs, which
// any user can read. wevtutil filters them, so only matching events come back.
//
//	6005 event log started (a boot)      6006 clean shut-down
//	6008 previous shut-down unexpected   41   Kernel-Power: rebooted without a clean shut-down
//	1074 who asked for a restart         1001 BugCheck (blue screen)
//	2004 low memory (Resource-Exhaustion-Detector)
//	Application: 1000 app crashed, 1002 app stopped responding
func readSignals(since time.Time) signals {
	var sig signals
	ms := time.Since(since).Milliseconds()
	sys := query("System", fmt.Sprintf("*[System[(EventID=6005 or EventID=6006 or EventID=6008 or EventID=41 or EventID=1074 or EventID=1001 or EventID=2004) and TimeCreated[timediff(@SystemTime) <= %d]]]", ms))
	if sys != nil {
		sig.knowsShutdowns = true
	}
	for _, e := range sys {
		t := e.time()
		switch {
		case e.ID == 6005:
			sig.boots = append(sig.boots, t)
		case e.ID == 6006:
			sig.shutdowns = append(sig.shutdowns, t)
		case e.ID == 6008 || (e.ID == 41 && strings.Contains(e.Provider(), "Kernel-Power")):
			sig.unexpected = append(sig.unexpected, t)
		case e.ID == 1074:
			sig.initiated = append(sig.initiated, restartBy(e, t))
		case e.ID == 1001 && strings.Contains(e.Provider(), "WER-SystemErrorReporting"):
			text := "Windows crashed with a blue screen and restarted."
			if code := e.data(0); code != "" {
				text += " Error code: " + strings.Fields(code)[0] + "."
			}
			sig.crashes = append(sig.crashes, sysEvent{t: t, text: text})
			sig.problems = append(sig.problems, Problem{Time: t.UnixMilli(), Kind: "system", Title: "Windows crashed (blue screen)", Detail: text})
		case e.ID == 2004:
			sig.problems = append(sig.problems, Problem{Time: t.UnixMilli(), Kind: "memory",
				Title: "Windows ran low on memory", Detail: "Windows warned that memory was nearly used up."})
		}
	}
	app := query("Application", fmt.Sprintf("*[System[(EventID=1000 or EventID=1002) and TimeCreated[timediff(@SystemTime) <= %d]]]", ms))
	for _, e := range app {
		name := oneLine(e.data(0))
		if name == "" {
			continue
		}
		p := Problem{Time: e.time().UnixMilli(), App: name}
		if e.ID == 1000 && strings.Contains(e.Provider(), "Application Error") {
			p.Kind, p.Title = "crash", name+" crashed"
		} else if e.ID == 1002 {
			p.Kind, p.Title = "hang", name+" stopped responding and was closed"
		} else {
			continue
		}
		sig.problems = append(sig.problems, p)
	}
	return sig
}

func restartBy(e winEvent, t time.Time) sysEvent {
	proc := oneLine(e.data(0)) // e.g. C:\WINDOWS\servicing\TrustedInstaller.exe (HOST)
	reason := oneLine(e.data(2))
	exe := filepath.Base(strings.TrimSpace(strings.SplitN(proc, " (", 2)[0]))
	low := strings.ToLower(exe + " " + reason)
	for _, k := range []string{"trustedinstaller", "mousocoreworker", "wuauclt", "usoclient", "upgrade", "service pack", "hotfix", "security fix"} {
		if strings.Contains(low, k) {
			return sysEvent{t: t, update: true, text: "Windows restarted to install updates."}
		}
	}
	text := "Restarted on request"
	if exe != "" && exe != "." {
		text += " by " + exe
	}
	if reason != "" {
		text += " (" + reason + ")"
	}
	return sysEvent{t: t, text: text + "."}
}

type winEvent struct {
	System struct {
		Provider struct {
			Name string `xml:"Name,attr"`
		} `xml:"Provider"`
		ID          int `xml:"EventID"`
		TimeCreated struct {
			SystemTime string `xml:"SystemTime,attr"`
		} `xml:"TimeCreated"`
	} `xml:"System"`
	Data []string `xml:"EventData>Data"`
	ID   int      `xml:"-"`
}

func (e winEvent) Provider() string { return e.System.Provider.Name }

func (e winEvent) time() time.Time {
	t, _ := time.Parse(time.RFC3339Nano, e.System.TimeCreated.SystemTime)
	return t.Local()
}

func (e winEvent) data(i int) string {
	if i < len(e.Data) {
		return strings.TrimSpace(e.Data[i])
	}
	return ""
}

// query returns nil when the log can't be read, so "no record" isn't mistaken for "nothing happened".
func query(logName, xpath string) []winEvent {
	out, err := runTool(20*time.Second, sysTool("wevtutil"), "qe", logName, "/q:"+xpath, "/f:xml", "/c:500", "/rd:true")
	if err != nil {
		return nil
	}
	events := []winEvent{}
	dec := xml.NewDecoder(bytes.NewReader(out))
	for {
		var e winEvent
		if err := dec.Decode(&e); err != nil {
			if err == io.EOF {
				break
			}
			break
		}
		e.ID = e.System.ID
		events = append(events, e)
	}
	return events
}
