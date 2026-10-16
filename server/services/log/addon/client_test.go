// Copyright 2026 Woodpecker Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package addon

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared_addon "go.woodpecker-ci.org/woodpecker/v3/shared/addon"
)

func TestLoadFailsForMissingExecutable(t *testing.T) {
	t.Parallel()

	service, err := Load(filepath.Join(t.TempDir(), "missing-log-addon"))
	require.Error(t, err, "a missing addon executable must fail startup")
	assert.Nil(t, service)

	// a failed initial load must not leave a registered, process-owning manager behind
	require.NoError(t, shared_addon.ShutdownAll(t.Context()))
}
