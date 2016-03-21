// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package api_test

import (
	"bytes"
	"io/ioutil"
	"net/http"

	jc "github.com/juju/testing/checkers"
	"github.com/juju/version"
	gc "gopkg.in/check.v1"
)

func (s *clientSuite) TestUploadGUIArchiveSuccess(c *gc.C) {
	otherSt, otherAPISt := s.otherEnviron(c)
	defer otherSt.Close()
	defer otherAPISt.Close()
	client := otherAPISt.Client()

	// Prepare a GUI archive.
	archive := []byte("archive content")
	hash, size, vers := "archive-hash", int64(len(archive)), version.MustParse("2.1.0")
	called := false

	// Set up a fake endpoint for tests.
	defer fakeAPIEndpoint(c, client, "/gui-archive", "POST",
		func(w http.ResponseWriter, req *http.Request) {
			defer req.Body.Close()
			called = true
			err := req.ParseForm()
			c.Assert(err, jc.ErrorIsNil)
			// Check version and content length.
			c.Assert(req.Form.Get("version"), gc.Equals, vers.String())
			c.Assert(req.ContentLength, gc.Equals, size)
			// Check request body.
			obtainedArchive, err := ioutil.ReadAll(req.Body)
			c.Assert(err, jc.ErrorIsNil)
			c.Assert(obtainedArchive, gc.DeepEquals, archive)
			// Check hash, and fail if hash is empty.
			h := req.Form.Get("hash")
			if h == "" {
				w.WriteHeader(http.StatusBadRequest)
			} else {
				c.Assert(h, gc.Equals, hash)
			}
		},
	).Close()

	// Check that the API client POSTs the GUI archive to the correct endpoint.
	err := client.UploadGUIArchive(bytes.NewReader(archive), hash, size, vers)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(called, jc.IsTrue)

	// Fail by passing an empty hash.
	err = client.UploadGUIArchive(bytes.NewReader(archive), "", size, vers)
	c.Assert(err, gc.ErrorMatches, "cannot upload the GUI archive: .*")
}
