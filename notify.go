package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ncruces/zenity"
)

// ---------------------------------------------------------------- pop-ups

type popper struct {
	open    func() // shows the dashboard
	act     func(pid int32, action string) error
	dialogs atomic.Bool // one dialog on screen at a time
}

// show puts an alert on screen. Warnings are ordinary notifications; critical
// alerts open a small dialog that offers to open TaskLens or pause the app.
func (p *popper) show(a Alert) {
	if !a.Critical || !p.dialogs.CompareAndSwap(false, true) {
		if err := zenity.Notify(a.Body, zenity.Title("TaskLens: "+a.Title)); err != nil {
			log.Printf("notification failed: %v", err)
		}
		return
	}
	defer p.dialogs.Store(false)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	opts := []zenity.Option{zenity.Title("TaskLens"), zenity.WarningIcon, zenity.Context(ctx),
		zenity.OKLabel("Open TaskLens"), zenity.CancelLabel("Dismiss")}
	if a.PID != 0 {
		opts = append(opts, zenity.ExtraButton("Pause "+short(a.App)))
	}
	err := zenity.Question(a.Title+"\n\n"+a.Body, opts...)
	switch {
	case err == nil:
		p.open()
	case errors.Is(err, zenity.ErrExtraButton):
		if err := p.act(a.PID, "pause"); err != nil {
			zenity.Notify(err.Error(), zenity.Title("TaskLens: couldn't pause "+a.App))
		} else {
			zenity.Notify("It's frozen, nothing is lost. Resume it from TaskLens when you're ready.",
				zenity.Title("TaskLens: paused "+a.App))
		}
	case errors.Is(err, zenity.ErrCanceled), errors.Is(err, context.DeadlineExceeded):
	default: // no dialog tool available (e.g. a minimal Linux desktop)
		zenity.Notify(a.Body, zenity.Title("TaskLens: "+a.Title))
	}
}

func short(s string) string {
	if len(s) > 24 {
		return s[:22] + "…"
	}
	return s
}

// ---------------------------------------------------------------- email

type mailer struct {
	settings *settingsStore
	mu       sync.Mutex
	sent     []time.Time
}

// Providers people use at home. All of them need an "app password", made in the
// account's security settings, rather than the normal password.
var providers = map[string]struct {
	Host string
	Port int
}{
	"gmail":  {"smtp.gmail.com", 587},
	"icloud": {"smtp.mail.me.com", 587},
	"yahoo":  {"smtp.mail.yahoo.com", 465},
}

const maxPerHour = 10 // never flood anyone's inbox

// alert emails one alert to the people who want it. It returns "sent", or why not.
func (m *mailer) alert(a Alert, snap Snapshot) string {
	set := m.settings.get()
	var to []string
	for _, r := range set.Email.Recipients {
		if a.Critical || !r.CriticalOnly {
			to = append(to, r.Email)
		}
	}
	if len(to) == 0 {
		return "nobody is set to receive this kind of alert"
	}
	m.mu.Lock()
	cut := time.Now().Add(-time.Hour)
	for len(m.sent) > 0 && m.sent[0].Before(cut) {
		m.sent = m.sent[1:]
	}
	if len(m.sent) >= maxPerHour {
		m.mu.Unlock()
		return "skipped: already sent 10 emails this hour"
	}
	m.sent = append(m.sent, time.Now())
	m.mu.Unlock()

	level := "Warning"
	if a.Critical {
		level = "Urgent"
	}
	s := snap.System
	subject := fmt.Sprintf("[TaskLens] %s on %s", a.Title, s.Host)
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n\n%s\n\n", level, a.Title, a.Body)
	fmt.Fprintf(&b, "Computer: %s (%s)\nTime: %s\n\n", s.Host, s.OS, time.UnixMilli(a.Time).Format("Mon 2 Jan 2006, 15:04"))
	if s.MemTotal > 0 {
		fmt.Fprintf(&b, "Right now: processor %.0f%% busy · memory %.0f%% (%s of %s) · swap %s · disk %s free\n",
			s.CPU, float64(s.MemUsed)/float64(s.MemTotal)*100, human(s.MemUsed), human(s.MemTotal), human(s.SwapUsed), human(s.DiskFree))
	}
	if len(snap.Procs) > 0 {
		b.WriteString("Biggest apps:\n")
		for i := 0; i < 5 && i < len(snap.Procs); i++ {
			p := snap.Procs[i]
			fmt.Fprintf(&b, "  • %s: %s memory, %.0f%% processor\n", p.Name, human(p.Mem), p.CPU)
		}
	}
	b.WriteString("\nWhat to do: open TaskLens on that computer to pause or close the app.\n\n" +
		"Sent by TaskLens. To change or stop these emails, open TaskLens → Alerts.\n")
	if err := m.send(set.Email, to, subject, b.String()); err != nil {
		log.Printf("alert email failed: %v", err)
		return "failed: " + err.Error()
	}
	return "sent"
}

// report emails the report to everyone on the list.
func (m *mailer) report(r Report) error {
	set := m.settings.get()
	var to []string
	for _, x := range set.Email.Recipients {
		to = append(to, x.Email)
	}
	if len(to) == 0 {
		return errors.New("add at least one person to the email list first")
	}
	period := fmt.Sprintf("last %d days", r.Days)
	if r.Days == 1 {
		period = "last 24 hours"
	}
	return m.send(set.Email, to, fmt.Sprintf("[TaskLens] Report for %s, %s", r.Host, period), r.text())
}

func (m *mailer) test() error {
	set := m.settings.get()
	var to []string
	for _, r := range set.Email.Recipients {
		to = append(to, r.Email)
	}
	if len(to) == 0 {
		return errors.New("add at least one person to alert first")
	}
	host, _ := hostname()
	body := "This is a test from TaskLens on " + host + ".\n\n" +
		"If you can read this, email alerts are working. You'll get an email like this when the computer " +
		"is about to run out of memory or disk space, or an app is using far too much.\n\n" +
		"Sent by TaskLens. To change or stop these emails, open TaskLens → Alerts.\n"
	return m.send(set.Email, to, "[TaskLens] Test email from "+host, body)
}

func (m *mailer) send(e EmailSettings, to []string, subject, body string) error {
	if e.Host == "" || e.Port == 0 || e.Username == "" {
		return errors.New("the sender account isn't set up yet")
	}
	pw := m.settings.password(e.Username)
	if pw == "" {
		return errors.New("the app password for " + e.Username + " hasn't been saved yet")
	}
	addr := net.JoinHostPort(e.Host, fmt.Sprint(e.Port))
	tlsCfg := &tls.Config{ServerName: e.Host, MinVersion: tls.VersionTLS12}
	dialer := &net.Dialer{Timeout: 20 * time.Second}
	var conn net.Conn
	var err error
	if e.Port == 465 {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("couldn't reach %s: %w", e.Host, err)
	}
	conn.SetDeadline(time.Now().Add(45 * time.Second))
	c, err := smtp.NewClient(conn, e.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if e.Port != 465 {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New(e.Host + " doesn't offer an encrypted connection; TaskLens won't send the password unencrypted")
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return err
		}
	}
	if err := c.Auth(smtp.PlainAuth("", e.Username, pw, e.Host)); err != nil {
		return fmt.Errorf("the email provider refused the sign-in. Check the address and that you used an app password, not your normal password (%v)", err)
	}
	if err := c.Mail(e.Username); err != nil {
		return err
	}
	for _, r := range to {
		if err := c.Rcpt(r); err != nil {
			return fmt.Errorf("%s was refused: %w", r, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	msg := "From: TaskLens <" + oneLine(e.Username) + ">\r\n" +
		"To: " + oneLine(strings.Join(to, ", ")) + "\r\n" +
		"Subject: " + mimeHeader(oneLine(subject)) + "\r\n" +
		"Date: " + time.Now().Format(time.RFC1123Z) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n\r\n" +
		strings.ReplaceAll(body, "\n", "\r\n")
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// mimeHeader encodes non-ASCII subjects (e.g. "→") so every mail app shows them.
func mimeHeader(s string) string {
	for _, r := range s {
		if r > 127 {
			return "=?utf-8?q?" + qEncode(s) + "?="
		}
	}
	return s
}

func qEncode(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		switch {
		case c == ' ':
			b.WriteByte('_')
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '.', c == '-':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "=%02X", c)
		}
	}
	return b.String()
}
