package plc

// ReadEach implements Driver.ReadMulti for test mocks by calling ReadTag per
// tag, so mocks that control ReadTag keep driving the manager's scan loop.
func ReadEach(d Driver, tags []string, dests []any) ([]error, error) {
	errs := make([]error, len(tags))
	for i, tag := range tags {
		errs[i] = d.ReadTag(tag, dests[i])
	}
	return errs, nil
}
