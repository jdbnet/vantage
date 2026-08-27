package sshx

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/jdbnet/vantage/internal/store"
	"golang.org/x/crypto/ssh"
)

type HostKeyInfo struct {
	Hostname    string
	Port        int
	Fingerprint string
	KeyType     string
	Status      string
	Previous    string
}

type HostKeyDecision struct {
	Accept  bool
	Replace bool
}

type Prompt struct {
	Output  func(text string)
	Line    func(echo bool) (string, error)
	HostKey func(info HostKeyInfo) (HostKeyDecision, error)
}

func (p *Prompt) print(text string) {
	if p != nil && p.Output != nil && text != "" {
		p.Output(text)
	}
}

func sshConfig(st *store.Store, creds Creds, prompt *Prompt, hostname string, port int) (*ssh.ClientConfig, error) {
	cfg := &ssh.ClientConfig{
		User: creds.User,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			return checkHostKey(st, prompt, hostname, port, key)
		},
		Timeout: 30 * time.Second,
		BannerCallback: func(message string) error {
			if prompt != nil {
				prompt.print(message)
				if !strings.HasSuffix(message, "\n") {
					prompt.print("\r\n")
				}
			}
			return nil
		},
	}

	kbd := ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
		return answerKbd(creds, prompt, name, instruction, questions, echos)
	})

	var methods []ssh.AuthMethod
	if creds.PrivateKey != "" {
		var signer ssh.Signer
		var err error
		if creds.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(creds.PrivateKey), []byte(creds.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(creds.PrivateKey))
		}
		if err != nil {
			return nil, err
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	// Keyboard-interactive before password so Duo/PAM challenge-response runs
	// instead of a password-only success that skips the second factor.
	if prompt != nil {
		methods = append(methods, ssh.RetryableAuthMethod(kbd, 5))
	} else {
		methods = append(methods, kbd)
	}
	if creds.Password != "" {
		methods = append(methods, ssh.Password(creds.Password))
	}
	cfg.Auth = methods
	return cfg, nil
}

func answerKbd(creds Creds, prompt *Prompt, name, instruction string, questions []string, echos []bool) ([]string, error) {
	var header strings.Builder
	if strings.TrimSpace(name) != "" {
		header.WriteString(strings.TrimSpace(name))
		header.WriteString("\r\n")
	}
	if strings.TrimSpace(instruction) != "" {
		instr := strings.ReplaceAll(instruction, "\n", "\r\n")
		header.WriteString(instr)
		if !strings.HasSuffix(instr, "\r\n") {
			header.WriteString("\r\n")
		}
	}
	if header.Len() > 0 {
		prompt.print(header.String())
	}
	if len(questions) == 0 {
		return []string{}, nil
	}

	answers := make([]string, len(questions))
	for i, q := range questions {
		if isSSHPasswordPrompt(q) && creds.Password != "" {
			answers[i] = creds.Password
			continue
		}
		if prompt == nil || prompt.Line == nil {
			return nil, fmt.Errorf("interactive authentication required: %s", strings.TrimSpace(q))
		}
		prompt.print(q)
		echo := true
		if i < len(echos) {
			echo = echos[i]
		}
		line, err := prompt.Line(echo)
		if err != nil {
			return nil, err
		}
		prompt.print("\r\n")
		answers[i] = line
	}
	return answers, nil
}

func isSSHPasswordPrompt(q string) bool {
	l := strings.ToLower(q)
	if strings.Contains(l, "passcode") || strings.Contains(l, "duo") || strings.Contains(l, "otp") || strings.Contains(l, "verification") {
		return false
	}
	return strings.Contains(l, "password")
}

func checkHostKey(st *store.Store, prompt *Prompt, hostname string, port int, key ssh.PublicKey) error {
	fp := ssh.FingerprintSHA256(key)
	keyType := key.Type()
	pub := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	stored, err := st.GetKnownHost(hostname, port)
	if err == nil && stored.Fingerprint == fp {
		return nil
	}
	info := HostKeyInfo{
		Hostname:    hostname,
		Port:        port,
		Fingerprint: fp,
		KeyType:     keyType,
		Status:      "new",
	}
	if err == nil {
		info.Status = "mismatch"
		info.Previous = stored.Fingerprint
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if prompt == nil || prompt.HostKey == nil {
		return fmt.Errorf("host key for %s:%d is not trusted", hostname, port)
	}
	dec, err := prompt.HostKey(info)
	if err != nil {
		return err
	}
	if !dec.Accept {
		return fmt.Errorf("host key rejected")
	}
	if info.Status == "mismatch" && !dec.Replace {
		return fmt.Errorf("host key mismatch")
	}
	_, err = st.UpsertKnownHost(hostname, port, keyType, fp, pub)
	return err
}
