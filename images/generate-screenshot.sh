#!/bin/bash

export GHMON_REVIEW_QUERY="is:open+is:pr+repo:kokke/tiny-aes-c+archived:false"
export GHMON_OWN_QUERY="is:open+is:pr+repo:nahojkap/ghmon-test-project+archived:false"

go run .