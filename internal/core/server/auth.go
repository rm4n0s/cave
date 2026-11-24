package server

import (
	"net"

	"github.com/rm4n0s/cave/internal/core/attempts"
	"github.com/rm4n0s/cave/internal/core/people"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// The names under which an authenticated connection's Permissions carry who it is.
const (
	extPersonID       = "cave-person-id"
	extPersonName     = "cave-person-name"
	extKeyID          = "cave-key-id"
	extKeyFingerprint = "cave-key-fingerprint"
)

// errDenied is what every refused key gets, whatever the reason: the client must not be able to
// tell an unknown name from a disabled person or a wrong key (docs/SPEC.md §7.3).
var errDenied = &errors.Error{Tag: "AuthDenied", Message: "permission denied"}

// tracker remembers what happened on one connection before it was authenticated, to name the
// outcome of the attempt (docs/SPEC.md §8.1). Its methods are called by the handshake, on the
// connection's goroutine.
type tracker struct {
	// reachedAuth says that the key exchange ended and authentication could start.
	reachedAuth bool
	// loginName and clientVersion are what the client sent, as it sent them.
	loginName, clientVersion string
	// keys are the fingerprints of the keys offered, at most maxKeysTracked.
	keys []string
	// offered counts the keys offered, also those past the ones remembered.
	offered int
	// last is the answer for the last key offered.
	last people.Result
	// acceptable is the answer for a key that is the person's, when one was offered.
	acceptable *people.Result
	// keyOwner is the name of another person who has one of the keys offered.
	keyOwner string
}

// offer notes a key that the client offered and the answer it got.
func (t *tracker) offer(key ssh.PublicKey, res people.Result) {
	t.offered++
	if len(t.keys) < maxKeysTracked {
		t.keys = append(t.keys, ssh.FingerprintSHA256(key))
	}
	t.last = res
	if res.Reason == people.Accepted && t.acceptable == nil {
		t.acceptable = &res
	}
	if res.KeyOwner != "" {
		t.keyOwner = res.KeyOwner
	}
}

// failed names the outcome of a connection that was not authenticated, by the order of
// docs/SPEC.md §8.1: a key that was the person's but was not proven, then the reason of the last
// key offered, else no key. lookupName finds the person a login name belongs to.
func (t *tracker) failed(address string, lookupName func(string) (people.Person, bool)) attempts.Attempt {
	a := attempts.Attempt{Address: address, ClientVersion: t.clientVersion, LoginName: t.loginName, Keys: t.keys, KeyOwner: t.keyOwner}
	switch {
	case !t.reachedAuth:
		a.Outcome = attempts.HandshakeFailed
	case t.acceptable != nil:
		a.Outcome = attempts.NotProven
		a.PersonID, a.PersonName = t.acceptable.PersonID, t.acceptable.PersonName
	case t.offered == 0:
		a.Outcome = attempts.NoKey
		if p, ok := lookupName(t.loginName); ok {
			a.PersonID, a.PersonName = p.ID, p.Name
		}
	default:
		a.PersonID, a.PersonName = t.last.PersonID, t.last.PersonName
		switch t.last.Reason {
		case people.UnknownName:
			a.Outcome = attempts.UnknownName
		case people.Disabled:
			a.Outcome = attempts.Disabled
		default:
			a.Outcome = attempts.WrongKey
		}
	}
	return a
}

// config returns the configuration of one connection's handshake: public key authentication
// only, against the people as they are when the key is offered.
func (s *Server) config(t *tracker) *ssh.ServerConfig {
	config := &ssh.ServerConfig{
		ServerVersion:           "SSH-2.0-Cave_" + s.opts.Version,
		MaxAuthTries:            MaxAuthTries,
		PublicKeyAuthAlgorithms: publicKeyAlgorithms,
		PreAuthConnCallback: func(conn ssh.ServerPreAuthConn) {
			t.reachedAuth = true
			t.clientVersion = string(conn.ClientVersion())
		},
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			t.loginName = conn.User()
			res := s.opts.People.Lookup(conn.User(), key)
			t.offer(key, res)
			if res.Reason != people.Accepted {
				return nil, errDenied
			}
			return &ssh.Permissions{Extensions: map[string]string{
				extPersonID:       res.PersonID,
				extPersonName:     res.PersonName,
				extKeyID:          res.KeyID,
				extKeyFingerprint: ssh.FingerprintSHA256(key),
			}}, nil
		},
		AuthLogCallback: func(conn ssh.ConnMetadata, _ string, _ error) {
			// Every authentication request comes by here, also "none" and the methods Cave does
			// not have: the login name is known from the first of them.
			t.loginName = conn.User()
		},
	}
	s.mu.Lock()
	config.AddHostKey(s.hostKey)
	s.mu.Unlock()
	return config
}

// serve runs one admitted connection: the handshake within the time limit, the record of the
// attempt, and then the connection itself until it ends.
func (s *Server) serve(conn net.Conn) {
	defer s.running.Done()
	defer s.forget(conn)
	address := remoteAddress(conn)

	// The time limit of docs/SPEC.md §9.2: closing the connection ends the handshake.
	grace := s.opts.Clock.AfterFunc(GraceTime, func() {
		_ = conn.Close() // the handshake sees the close; nothing else to do with its error
	})
	t := &tracker{}
	sshConn, channels, requests, err := ssh.NewServerConn(conn, s.config(t))
	grace.Stop()
	s.settled(address)
	if err != nil {
		// The library closed the connection. Its error is the client's doing; the attempt's
		// outcome says what matters of it.
		s.record(t.failed(address, s.opts.People.ByName))
		return
	}
	perms := sshConn.Permissions.Extensions
	s.record(attempts.Attempt{
		Outcome: attempts.Accepted, PersonID: perms[extPersonID], PersonName: perms[extPersonName], LoginName: sshConn.User(),
		Address: address, ClientVersion: string(sshConn.ClientVersion()), Keys: []string{perms[extKeyFingerprint]},
	})
	s.serveAuthenticated(sshConn, channels, requests, address)
}
