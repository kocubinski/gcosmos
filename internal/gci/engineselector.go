package gci

// Cheap toggle at build time to quickly switch to running comet,
// for cases where the difference in behavior needs to be inspected.
//
// This is referenced in gcosmos/main.go and in gcosmos/main_test.go.
// Once we are on a stable tagged release, this constant will go away.
const RunCometInsteadOfGordian = false
