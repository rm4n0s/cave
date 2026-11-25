package sftp

import (
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/sshtest"
)

func TestTransfers(t *testing.T) {
	type step func(t *testing.T, s *testSession)
	open := func(path string, flags uint32) func(s *testSession) string {
		return func(s *testSession) string { return s.Open(path, flags, sshtest.SFTPAttrs{}) }
	}
	tests := []struct {
		name string
		run  step
		want []Transfer // without the times, which the test checks apart
	}{
		{"an upload", func(t *testing.T, s *testSession) {
			h := open("/home/tester/up.txt", OpenWrite|OpenCreate|OpenTrunc)(s)
			s.Write(h, 0, []byte("hello "))
			s.Write(h, 6, []byte("world"))
			s.Close(h)
		}, []Transfer{{Path: "/home/tester/up.txt", Uploaded: 11, Created: true, Result: ResultDone}}},
		{"a download, read to the end", func(t *testing.T, s *testSession) {
			h := open("/home/tester/down.txt", OpenRead)(s)
			s.Read(h, 0, 4)
			s.Read(h, 4, 100)
			s.Read(h, 9, 100) // the end of the file
			s.Close(h)
		}, []Transfer{{Path: "/home/tester/down.txt", Downloaded: 9, Result: ResultDone}}},
		{"read and write on one handle", func(t *testing.T, s *testSession) {
			h := open("/home/tester/down.txt", OpenRead|OpenWrite)(s)
			s.Read(h, 0, 3)
			s.Write(h, 0, []byte("ab"))
			s.Close(h)
		}, []Transfer{{Path: "/home/tester/down.txt", Downloaded: 3, Uploaded: 2, Result: ResultDone}}},
		{"an empty file created", func(t *testing.T, s *testSession) {
			s.Close(open("/home/tester/empty", OpenWrite|OpenCreate)(s))
		}, []Transfer{{Path: "/home/tester/empty", Created: true, Result: ResultDone}}},
		{"opened and closed without reading or writing", func(t *testing.T, s *testSession) {
			s.Close(open("/home/tester/down.txt", OpenRead)(s))
		}, nil},
		{"a read past the end only", func(t *testing.T, s *testSession) {
			h := open("/home/tester/down.txt", OpenRead)(s)
			s.Read(h, 100, 10)
			s.Close(h)
		}, nil},
		{"a write that fails", func(t *testing.T, s *testSession) {
			h := open("/home/tester/up.txt", OpenWrite|OpenCreate)(s)
			s.Write(h, 0, []byte("ok"))
			s.Write(h, 2<<20, []byte("too far"))
			s.Close(h)
		}, []Transfer{{Path: "/home/tester/up.txt", Uploaded: 2, Created: true, Result: ResultFailed}}},
		{"a read that fails", func(t *testing.T, s *testSession) {
			h := open("/home/tester/down.txt", OpenWrite)(s)
			s.Read(h, 0, 10)
			s.Write(h, 0, []byte("x"))
			s.Close(h)
		}, []Transfer{{Path: "/home/tester/down.txt", Uploaded: 1, Result: ResultFailed}}},
		{"a relative path is made absolute", func(t *testing.T, s *testSession) {
			h := open("notes/../up.txt", OpenWrite|OpenCreate)(s)
			s.Write(h, 0, []byte("x"))
			s.Close(h)
		}, []Transfer{{Path: "/home/tester/up.txt", Uploaded: 1, Created: true, Result: ResultDone}}},
		{"folders are not transfers", func(t *testing.T, s *testSession) {
			h := s.Call(typeOpendir, "/home/tester")
			if h.Type != typeHandle {
				t.Fatalf("opendir answered %d", h.Type)
			}
			s.Close(h.Handle())
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newMemFS()
			fs.put("/home/tester/down.txt", "123456789")
			s := start(t, fs)
			tt.run(t, s)
			s.clock.Advance(time.Minute) // the closing time is that of the close, not of the end
			if err := s.end(); err != nil {
				t.Fatal(err)
			}
			got := s.transfers()
			if len(got) != len(tt.want) {
				t.Fatalf("transfers %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if !got[i].OpenedAt.Equal(testStart) || !got[i].ClosedAt.Equal(testStart) {
					t.Errorf("opened %v, closed %v; want both at the start", got[i].OpenedAt, got[i].ClosedAt)
				}
				got[i].OpenedAt, got[i].ClosedAt = time.Time{}, time.Time{}
				if got[i] != tt.want[i] {
					t.Errorf("transfer %+v, want %+v", got[i], tt.want[i])
				}
			}
		})
	}
}

func TestATransferOpenAtTheEndIsInterrupted(t *testing.T) {
	fs := newMemFS()
	fs.put("/home/tester/down.txt", "123456789")
	s := start(t, fs)
	h := s.Open("/home/tester/down.txt", OpenRead, sshtest.SFTPAttrs{})
	s.Read(h, 0, 5)
	s.Open("/home/tester/other", OpenRead|OpenCreate, sshtest.SFTPAttrs{}) // created, never used
	s.clock.Advance(time.Minute)
	if err := s.end(); err != nil {
		t.Fatal(err)
	}
	got := s.transfers()
	if len(got) != 2 {
		t.Fatalf("transfers %+v", got)
	}
	byPath := map[string]Transfer{got[0].Path: got[0], got[1].Path: got[1]}
	down := byPath["/home/tester/down.txt"]
	if down.Result != ResultInterrupted || down.Downloaded != 5 || !down.ClosedAt.Equal(testStart.Add(time.Minute)) {
		t.Errorf("the open download is %+v", down)
	}
	if other := byPath["/home/tester/other"]; other.Result != ResultInterrupted || !other.Created {
		t.Errorf("the created file is %+v", other)
	}
}
