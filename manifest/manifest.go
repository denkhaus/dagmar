package manifest

// Version is the manifest contract version this library implements (CRD-analogous: the platform
// pins which schema revision it validates/speaks). SemVer; bump on incompatible manifest changes.
// The platform module references this to declare the contract version it conforms to.
const Version = "v0.1.0"
