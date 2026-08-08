// Package tools holds dagmar's reusable COMPOSED tool units — building blocks assembled
// into an Agent's tool-set (dag.git / container / http + Project Hook Service exposures).
// These are the cross-workflow-reusable units (ADR-0010 §4); extract to a separate Dagger
// module only when reuse crosses the dagmar boundary.
package tools
