// The JavaScript native gate's config (issue #547). ESLint is the language's
// standard analyzer and ships no config of its own, so "default strictness"
// here is its own recommended set and nothing added: js.configs.recommended,
// over the generated ES modules of both corpora, every rule at the severity
// ESLint ships it at.
//
// Beyond that this file states only what the emitted files ARE. They are ES
// modules at the current language level. They name no browser and no Node
// global except TextEncoder and TextDecoder, which are WHATWG globals every
// JavaScript host the generated code targets provides — declaring them is
// telling ESLint what the environment is, not excusing a finding.
import js from "@eslint/js";

export default [
    js.configs.recommended,
    {
        files: ["**/*.js", "**/*.mjs"],
        languageOptions: {
            ecmaVersion: "latest",
            sourceType: "module",
            globals: {
                TextDecoder: "readonly",
                TextEncoder: "readonly",
            },
        },
    },
];
