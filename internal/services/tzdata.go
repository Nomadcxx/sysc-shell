// Package services: time/tzdata embeds the IANA zone database into every
// binary that imports this package, so zone lookups stop reading the host's
// /usr/share/zoneinfo. The sun and night-light tests here, and the weather
// tests in internal/shell, then run on any machine instead of skipping when
// the host has no system zone data.
package services

import _ "time/tzdata"
