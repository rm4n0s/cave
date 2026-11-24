// Package wine tells a test whether it runs under Wine, so that a test of something Wine does not
// do (docs/SPEC.md §13.6) can skip itself and say why. Nothing but tests uses it.
package wine
