import js from "@eslint/js";
import css from "@eslint/css";
import globals from "globals";

const sharedFunctions = [
  "h", "withProject", "api", "send", "itemURL", "putItem", "deleteItem", "clone", "kindOf", "duration", "seconds", "ago",
  "money", "fileURL", "loadLibrary", "loadRuns", "loadProjects", "renderProjects", "switchProject", "renderSide",
  "createItem", "closeStream", "refresh", "field", "textInput", "textArea", "selectBox", "linesInput", "rowsEditor",
  "mapToRows", "rowsToMap", "usedBy", "callersOf", "usedByLine", "listsPassed", "checkLater", "draftStatus",
  "showHome", "waitingCard", "chainCard", "runList",
  "readCondition", "writeCondition", "describeCondition", "conditionEditor",
  "notApproved", "ifGroups", "outlineOf", "stepsIn", "sourceOf", "goLink", "sourceNote", "conditionLabel", "renderOutline",
  "skeleton", "valueNow", "markBlocks",
  "chainProblems", "startForm", "approvalBefore",
  "showEditor", "touch", "renderSaveBar", "removeChain", "saveChain", "treeRow", "subchainRows", "renderEditorTree", "select",
  "slugOf", "uniqueID", "addStep", "move", "removeStep", "renderInspector", "rootInspector",
  "fieldsOf", "stepInspector", "inspectorSection", "readsStrip", "withReads", "valuesGroup", "setsSection", "flowFields", "sharedEditor", "kindFields", "agentFields", "toolValues", "valuesEditor",
  "toolPicker", "promptPicker",
  "itemButtons", "showTool", "showSettings", "showMemory", "returnsList", "showPrompt",
  "stepFields", "readsOf", "readsList", "readsPanel", "readersOf", "earlierPanel", "remembered", "openAsk", "questionsForm", "asksPanel", "block", "actionLabel", "callView", "callPanel",
  "stepShows", "renderArtifacts", "diffView", "prettyJSON", "openLightbox",
  "showRun", "findStepIn", "renderRun", "outcomeBanner", "renderDecision", "focusRow", "syncRows", "redrawRow", "toggleRow",
  "stepRow", "stepStats", "rowStats", "setShowIdle", "idleNote", "progressOf", "itemTitle", "startsAround", "rerunPoints", "valueLabel", "summaryLine", "roundsOf", "syncRounds", "pastRound", "pastStep", "stepBody", "editLinks", "decideForm", "stopRun", "deleteRun",
];

const sharedConstants = [
  "main", "lightbox", "EXT", "KIND_LETTER", "KIND_NAME", "STATE_ICON", "STATUS_LABEL", "money", "FROM", "TAG",
  "folded", "NEW_PROMPT", "placeholdersIn", "returnsText", "mapToRows", "rowsToMap", "keyOf", "sizeOf", "shellWord",
  "firstLine", "CONDITION_OPS", "KIND_CHOICES", "usesCount", "draftSteps", "unsaved", "RETURN_EXAMPLE", "RAN", "editorGo", "runGo",
  "holdsSteps", "MAX_PARALLEL", "perItem", "NOT_RAN", "roundOf", "asText", "LONE_PLACEHOLDER", "folderName", "promptOf", "ASKED_AGAIN", "ASKED_AGAIN_TITLE", "LOOP_ENDS", "LEFT_TO_AGENT", "answerOf",
];

const sharedState = ["lib", "projects", "project", "runsCache", "stream", "current", "editor", "runView", "lastHash", "lightboxState", "showIdle"];

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
    files: ["test/**/*.mjs"],
    languageOptions: {
      ecmaVersion: 2024,
      sourceType: "module",
      globals: globals.node,
    },
    rules: { ...js.configs.recommended.rules, ...strictRules },
  },
  {
    files: ["src/web/**/*.css"],
    plugins: { css },
    language: "css/css",
    rules: { ...css.configs.recommended.rules, "css/use-baseline": "off" },
  },
  {
    files: ["src/web/run.css"],
    rules: { "css/no-invalid-properties": ["error", { allowUnknownVariables: true }] },
  },
];
