module.exports = {
    plugins: [
        "@semantic-release/commit-analyzer",
        "@semantic-release/release-notes-generator",
        ["@semantic-release/github", {
            "assets": [
                {"path": "./artifacts/orbctl-Darwin-x86_64", "label": "Darwin x86_64"},
                {"path": "./artifacts/orbctl-Darwin-ARM64", "label": "Darwin ARM64"},
                {"path": "./artifacts/orbctl-FreeBSD-x86_64", "label": "FreeBSD x86_64"},
                {"path": "./artifacts/orbctl-Linux-x86_64", "label": "Linux x86_64"},
                {"path": "./artifacts/orbctl-OpenBSD-x86_64", "label": "OpenBSD x86_64"},
                {"path": "./artifacts/orbctl-Windows-x86_64.exe", "label": "Windows x86_64"},
            ]
        }]
    ]
};