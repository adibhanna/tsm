package session

import "bytes"

var (
	switchSeqPrefix = []byte("\x1b]633;TSM_ATTACH=")
	switchSeqSuffix = byte('\a')
)

type SwitchSessionError struct {
	Target string
}

func (e *SwitchSessionError) Error() string {
	return "switch session to " + e.Target
}

func AttachSwitchSequence(target string) string {
	return string(switchSeqPrefix) + target + string(switchSeqSuffix)
}

type outputFilter struct {
	pending []byte
	buf     []byte // reusable scratch buffers
	out     []byte
}

func (f *outputFilter) Filter(data []byte) ([]byte, string) {
	needed := len(f.pending) + len(data)
	if cap(f.buf) < needed {
		f.buf = make([]byte, 0, needed)
	}
	buf := f.buf[:0]
	buf = append(buf, f.pending...)
	buf = append(buf, data...)
	f.pending = f.pending[:0]

	if cap(f.out) < len(buf) {
		f.out = make([]byte, 0, cap(buf))
	}
	out := f.out[:0]
	target := ""

	for len(buf) > 0 {
		idx := bytes.Index(buf, switchSeqPrefix)
		if idx < 0 {
			keep := partialPrefixLen(buf, switchSeqPrefix)
			if keep > 0 {
				out = append(out, buf[:len(buf)-keep]...)
				f.pending = append(f.pending, buf[len(buf)-keep:]...)
			} else {
				out = append(out, buf...)
			}
			break
		}

		out = append(out, buf[:idx]...)
		buf = buf[idx+len(switchSeqPrefix):]

		end := bytes.IndexByte(buf, switchSeqSuffix)
		if end < 0 {
			f.pending = append(f.pending, switchSeqPrefix...)
			f.pending = append(f.pending, buf...)
			break
		}

		if target == "" {
			target = string(buf[:end])
		}
		buf = buf[end+1:]
	}

	return out, target
}

func partialPrefixLen(buf, prefix []byte) int {
	max := len(prefix) - 1
	if max > len(buf) {
		max = len(buf)
	}
	for i := max; i > 0; i-- {
		if bytes.Equal(buf[len(buf)-i:], prefix[:i]) {
			return i
		}
	}
	return 0
}
