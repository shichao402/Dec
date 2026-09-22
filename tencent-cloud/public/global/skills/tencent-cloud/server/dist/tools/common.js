import { redactSecrets, resolveCredentials, resolveProjectRoot } from "../lib.js";
export function textResult(payload) {
    return {
        content: [{ type: "text", text: JSON.stringify(payload, null, 2) }]
    };
}
export function createToolContext() {
    const projectRoot = resolveProjectRoot();
    return {
        projectRoot,
        getCredentials: () => resolveCredentials(projectRoot)
    };
}
export function safeResult(payload) {
    return textResult(redactSecrets(payload));
}
//# sourceMappingURL=common.js.map