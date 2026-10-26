import js from "@eslint/js";
import css from "@eslint/css";
import globals from "globals";

const sharedFunctions = [
  "h", "withProject", "api", "send", "itemURL", "putItem", "deleteItem", "clone", "kindOf", "duration", "ago",
  "money", "fileURL", "loadLibrary", "loadRuns", "loadProjects", "renderProjects", "switchProject", "renderSide",
  "createItem", "closeStream", "refresh", "field", "textInput", "textArea", "selectBox", "linesInput", "rowsEditor",
  "mapToRows", "rowsToMap", "usedBy", "showChain", "touch", "renderChainPage", "renderSaveBar", "removeChain",
  "saveChain", "treeRow", "subchainRows", "renderEditorTree", "select", "slugOf", "uniqueID", "addStep", "move", "removeStep",
  "renderInspector", "rootInspector", "stepInspector", "kindFields", "toolValues", "valuesEditor", "picked", "toolPicker",
  "promptPicker", "itemButtons",
  "showTool", "showSettings", "showMemory", "returnsList", "showPrompt", "showRun", "renderRun", "runTreeRows", "keyOf", "focusCard",
  "syncCards", "stepFields", "earlierPanel", "sizeOf", "shellWord", "elapsed", "remembered", "block", "actionLabel",
  "callView", "callPanel", "stepCard", "editLinks", "decideForm", "renderArtifacts", "diffView", "prettyJSON",
  "openLightbox", "stopRun",
];

const sharedConstants = [
  "main", "lightbox", "EXT", "KIND_LETTER", "KIND_NAME", "STATE_ICON", "STATUS_LABEL", "money", "FROM", "TAG",
  "folded", "NEW_PROMPT", "placeholdersIn", "returnsText", "mapToRows", "rowsToMap", "keyOf", "sizeOf", "shellWord",
];

const sharedState = ["lib", "projects", "project", "runsCache", "stream", "current", "editor", "runView", "lastHash", "lightboxState"];

const sharedGlobals = Object.fromEntries([
  ...sharedFunctions.map((name) => [name, "readonly"]),
  ...sharedConstants.map((name) => [name, "readonly"]),
  ...sharedState.map((name) => [name, "writable"]),
]);

const strictRules = {
  "curly": ["error", "multi-line"],
  "eqeqeq": ["error", "always"],
  "no-alert": "off",
  "no-console": "error",
  "no-eval": "error",
  "no-implied-eval": "error",
  "no-new-func": "error",
  "no-script-url": "error",
  "no-var": "error",
  "prefer-const": "error",
  "prefer-template": "error",
  "object-shorthand": "error",
  "prefer-arrow-callback": "error",
  "arrow-body-style": ["error", "as-needed"],
  "no-else-return": "error",
  "no-lonely-if": "error",
  "no-useless-concat": "error",
  "no-useless-return": "error",
  "no-unneeded-ternary": "error",
  "no-implicit-coercion": ["error", { boolean: false }],
  "no-unused-expressions": ["error", { allowTernary: true, allowShortCircuit: true }],
  "no-empty": ["error", { allowEmptyCatch: true }],
  "no-redeclare": ["error", { builtinGlobals: false }],
  "no-unused-vars": ["error", { vars: "local", args: "after-used" }],
  "no-param-reassign": "off",
  "no-nested-ternary": "off",
  "no-use-before-define": "off",
  "no-await-in-loop": "off",
  "max-depth": ["error", 5],
  "max-params": ["error", 6],
  "complexity": ["error", 30],
  "semi": ["error", "always"],
  "quotes": ["error", "double", { avoidEscape: true, allowTemplateLiterals: true }],
  "no-multiple-empty-lines": ["error", { max: 1, maxBOF: 0, maxEOF: 0 }],
  "no-trailing-spaces": "error",
  "eol-last": "error",
};

export default [
  {
    ignores: ["node_modules/**", "src/samples/**"],
  },
  {
    files: ["src/web/**/*.js"],
    languageOptions: {
      ecmaVersion: 2024,
      sourceType: "script",
      globals: { ...globals.browser, ...sharedGlobals },
    },
    rules: { ...js.configs.recommended.rules, ...strictRules },
  },
  {
    files: ["src/web/**/*.css"],
    plugins: { css },
    language: "css/css",
    rules: { ...css.configs.recommended.rules, "css/use-baseline": "off" },
  },
];
