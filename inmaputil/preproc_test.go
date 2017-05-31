/*
Copyright © 2017 the InMAP authors.
This file is part of InMAP.

InMAP is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

InMAP is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with InMAP.  If not, see <http://www.gnu.org/licenses/>.
*/

package inmaputil

import (
	"testing"
)

func TestPreprocWRFChem(t *testing.T) {
	Cfg.SetConfigFile("../inmap/configExampleWRFChem.toml")
	cfg, err := LoadConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	// Here we only test whether the program runs. We
	// check whether the output is correct elsewhere.
	if err = Preproc(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestPreprocGEOSChem(t *testing.T) {
	Cfg.SetConfigFile("../inmap/configExampleGEOSChem.toml")
	cfg, err := LoadConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	// Here we only test whether the program runs. We
	// check whether the output is correct elsewhere.
	if err = Preproc(cfg); err != nil {
		t.Fatal(err)
	}
}
