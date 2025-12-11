import * as vscode from 'vscode';
import { NtmClient, RobotStatus, SessionInfo } from './ntmClient';

export class NtmDashboard {
    public static currentPanel: NtmDashboard | undefined;
    private readonly _panel: vscode.WebviewPanel;
    private readonly _extensionUri: vscode.Uri;
    private _disposables: vscode.Disposable[] = [];
    private _client: NtmClient;

    public static createOrShow(extensionUri: vscode.Uri, client: NtmClient) {
        const column = vscode.window.activeTextEditor
            ? vscode.window.activeTextEditor.viewColumn
            : undefined;

        if (NtmDashboard.currentPanel) {
            NtmDashboard.currentPanel._panel.reveal(column);
            return;
        }

        const panel = vscode.window.createWebviewPanel(
            'ntmDashboard',
            'NTM Dashboard',
            column || vscode.ViewColumn.One,
            {
                enableScripts: true,
                localResourceRoots: [vscode.Uri.joinPath(extensionUri, 'media')]
            }
        );

        NtmDashboard.currentPanel = new NtmDashboard(panel, extensionUri, client);
    }

    private constructor(panel: vscode.WebviewPanel, extensionUri: vscode.Uri, client: NtmClient) {
        this._panel = panel;
        this._extensionUri = extensionUri;
        this._client = client;

        this._update();

        this._panel.onDidDispose(() => this.dispose(), null, this._disposables);

        this._panel.webview.onDidReceiveMessage(
            message => {
                switch (message.command) {
                    case 'refresh':
                        this._update();
                        return;
                    case 'spawn':
                        vscode.commands.executeCommand('ntm.spawn');
                        return;
                }
            },
            null,
            this._disposables
        );
        
        // Auto-refresh every 5s
        const interval = setInterval(() => this._update(), 5000);
        this._disposables.push({ dispose: () => clearInterval(interval) });
    }

    public dispose() {
        NtmDashboard.currentPanel = undefined;
        this._panel.dispose();
        while (this._disposables.length) {
            const x = this._disposables.pop();
            if (x) {
                x.dispose();
            }
        }
    }

    private async _update() {
        try {
            const status = await this._client.getStatus();
            this._panel.webview.html = this._getHtmlForWebview(status);
        } catch (e) {
            console.error(e);
        }
    }

    private _getHtmlForWebview(status: RobotStatus) {
        const style = `
            body { 
                font-family: var(--vscode-font-family); 
                padding: 20px; 
                color: var(--vscode-editor-foreground);
                background-color: var(--vscode-editor-background);
            }
            .grid { 
                display: grid; 
                grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); 
                gap: 20px; 
            }
            .card { 
                background-color: var(--vscode-sideBar-background); 
                border: 1px solid var(--vscode-widget-border); 
                padding: 15px; 
                border-radius: 6px;
            }
            .card-header {
                font-weight: bold;
                margin-bottom: 10px;
                display: flex;
                justify-content: space-between;
                align-items: center;
            }
            .agent-list {
                margin-top: 10px;
            }
            .agent {
                display: flex;
                align-items: center;
                margin-bottom: 5px;
                padding: 4px;
                background-color: var(--vscode-editor-lineHighlightBackground);
                border-radius: 4px;
            }
            .status-dot {
                width: 8px;
                height: 8px;
                border-radius: 50%;
                margin-right: 8px;
            }
            .status-active { background-color: var(--vscode-testing-iconPassed); }
            .status-inactive { background-color: var(--vscode-testing-iconSkipped); }
            
            .refresh-btn {
                background-color: var(--vscode-button-background);
                color: var(--vscode-button-foreground);
                border: none;
                padding: 8px 16px;
                cursor: pointer;
                border-radius: 4px;
            }
            .refresh-btn:hover {
                background-color: var(--vscode-button-hoverBackground);
            }
            .tag {
                font-size: 0.8em;
                padding: 2px 6px;
                border-radius: 10px;
                background-color: var(--vscode-badge-background);
                color: var(--vscode-badge-foreground);
                margin-left: 5px;
            }
        `;

        const sessionsHtml = status.sessions.map(s => this._renderSessionCard(s)).join('');

        return `<!DOCTYPE html>
            <html lang="en">
            <head>
                <meta charset="UTF-8">
                <style>${style}</style>
            </head>
            <body>
                <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px;">
                    <h1>NTM Dashboard</h1>
                    <div>
                        <button class="refresh-btn" onclick="vscode.postMessage({command: 'spawn'})">Spawn Session</button>
                        <button class="refresh-btn" onclick="vscode.postMessage({command: 'refresh'})">Refresh</button>
                    </div>
                </div>
                
                <div class="summary">
                    Total Sessions: ${status.summary.total_sessions} | 
                    Total Agents: ${status.summary.total_agents}
                    ${status.beads?.available ? `| Beads: ${status.beads.open} open, ${status.beads.in_progress} active` : ''}
                    ${status.agent_mail?.available ? `| Mail: Online` : ''}
                </div>
                <br/>

                <div class="grid">
                    ${sessionsHtml}
                </div>

                <script>
                    const vscode = acquireVsCodeApi();
                </script>
            </body>
            </html>`;
    }

    private _renderSessionCard(session: SessionInfo): string {
        const agentsHtml = session.agents?.map(a => {
            const statusClass = a.is_active ? 'status-active' : 'status-inactive';
            return `<div class="agent">
                <div class="status-dot ${statusClass}"></div>
                <span>${a.pane}</span>
                <span class="tag">${a.type}</span>
            </div>`;
        }).join('') || 'No agents';

        const attachedTag = session.attached 
            ? `<span class="tag" style="background-color: var(--vscode-testing-iconPassed)">Attached</span>` 
            : `<span class="tag">Detached</span>`;

        return `
            <div class="card">
                <div class="card-header">
                    <span>${session.name}</span>
                    ${attachedTag}
                </div>
                <div class="agent-list">
                    ${agentsHtml}
                </div>
            </div>
        `;
    }
}
