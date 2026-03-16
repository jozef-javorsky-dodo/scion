/**
 * Copyright 2026 Google LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

/**
 * Grove settings page component
 *
 * Displays grove-scoped templates, environment variables, secrets, and danger-zone actions (delete).
 */

import { LitElement, html, css } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';

import type { PageData, Grove, Template, AdminGroup } from '../../shared/types.js';
import { can, canAny } from '../../shared/types.js';
import { apiFetch } from '../../client/api.js';
import '../shared/env-var-list.js';
import '../shared/secret-list.js';
import '../shared/shared-dir-list.js';
import '../shared/group-member-editor.js';

interface Agent {
  id: string;
  phase: string;
  activity?: string;
}

@customElement('scion-page-grove-settings')
export class ScionPageGroveSettings extends LitElement {
  @property({ type: Object })
  pageData: PageData | null = null;

  @property({ type: String })
  groveId = '';

  @state()
  private loading = true;

  @state()
  private grove: Grove | null = null;

  @state()
  private error: string | null = null;

  @state()
  private deleteLoading = false;

  @state()
  private deleteAlsoAgents = false;

  @state()
  private templates: Template[] = [];

  @state()
  private templatesLoading = true;

  @state()
  private syncLoading = false;

  @state()
  private syncError: string | null = null;

  @state()
  private syncSuccess: string | null = null;

  @state()
  private membersGroup: AdminGroup | null = null;

  private syncAgentId: string | null = null;
  private syncPollTimer: ReturnType<typeof setInterval> | null = null;

  static override styles = css`
    :host {
      display: block;
    }

    .back-link {
      display: inline-flex;
      align-items: center;
      gap: 0.5rem;
      color: var(--scion-text-muted, #64748b);
      text-decoration: none;
      font-size: 0.875rem;
      margin-bottom: 1rem;
    }

    .back-link:hover {
      color: var(--scion-primary, #3b82f6);
    }

    .header {
      display: flex;
      align-items: center;
      gap: 0.75rem;
      margin-bottom: 2rem;
    }

    .header sl-icon {
      color: var(--scion-primary, #3b82f6);
      font-size: 1.5rem;
    }

    .header h1 {
      font-size: 1.5rem;
      font-weight: 700;
      color: var(--scion-text, #1e293b);
      margin: 0;
    }

    .section {
      background: var(--scion-surface, #ffffff);
      border: 1px solid var(--scion-border, #e2e8f0);
      border-radius: var(--scion-radius-lg, 0.75rem);
      padding: 1.5rem;
      margin-bottom: 1.5rem;
    }

    .section h2 {
      font-size: 1.125rem;
      font-weight: 600;
      color: var(--scion-text, #1e293b);
      margin: 0 0 0.25rem 0;
    }

    .section p {
      font-size: 0.875rem;
      color: var(--scion-text-muted, #64748b);
      margin: 0 0 1rem 0;
    }

    .section-header {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 1rem;
    }

    .section-header-text {
      flex: 1;
    }

    .template-list {
      display: flex;
      flex-direction: column;
      gap: 0.5rem;
    }

    .template-item {
      display: flex;
      align-items: center;
      gap: 0.75rem;
      padding: 0.75rem 1rem;
      background: var(--scion-bg-subtle, #f8fafc);
      border: 1px solid var(--scion-border, #e2e8f0);
      border-radius: var(--scion-radius, 0.5rem);
    }

    .template-item sl-icon {
      color: var(--scion-primary, #3b82f6);
      font-size: 1.125rem;
      flex-shrink: 0;
    }

    .template-info {
      flex: 1;
      min-width: 0;
    }

    .template-name {
      font-weight: 600;
      font-size: 0.875rem;
      color: var(--scion-text, #1e293b);
    }

    .template-meta {
      font-size: 0.75rem;
      color: var(--scion-text-muted, #64748b);
      margin-top: 0.125rem;
    }

    .template-badge {
      font-size: 0.6875rem;
      padding: 0.125rem 0.5rem;
      border-radius: 9999px;
      background: var(--scion-bg-subtle, #f1f5f9);
      color: var(--scion-text-muted, #64748b);
      border: 1px solid var(--scion-border, #e2e8f0);
      white-space: nowrap;
    }

    .empty-templates {
      text-align: center;
      padding: 2rem 1rem;
      color: var(--scion-text-muted, #64748b);
      font-size: 0.875rem;
    }

    .empty-templates sl-icon {
      font-size: 2rem;
      margin-bottom: 0.5rem;
      display: block;
    }

    .sync-status {
      display: flex;
      align-items: center;
      gap: 0.5rem;
      padding: 0.75rem 1rem;
      border-radius: var(--scion-radius, 0.5rem);
      font-size: 0.8125rem;
      margin-bottom: 1rem;
    }

    .sync-status.error {
      background: var(--sl-color-danger-50, #fef2f2);
      color: var(--sl-color-danger-700, #b91c1c);
      border: 1px solid var(--sl-color-danger-200, #fecaca);
    }

    .sync-status.success {
      background: var(--sl-color-success-50, #f0fdf4);
      color: var(--sl-color-success-700, #15803d);
      border: 1px solid var(--sl-color-success-200, #bbf7d0);
    }

    .sync-status.syncing {
      background: var(--sl-color-primary-50, #eff6ff);
      color: var(--sl-color-primary-700, #1d4ed8);
      border: 1px solid var(--sl-color-primary-200, #bfdbfe);
    }

    .danger-section {
      border-color: var(--sl-color-danger-200, #fecaca);
    }

    .danger-section h2 {
      color: var(--sl-color-danger-600, #dc2626);
    }

    .delete-area {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 1.5rem;
      padding-top: 1rem;
      border-top: 1px solid var(--scion-border, #e2e8f0);
    }

    .delete-info {
      flex: 1;
    }

    .delete-info h3 {
      font-size: 0.9375rem;
      font-weight: 600;
      color: var(--scion-text, #1e293b);
      margin: 0 0 0.25rem 0;
    }

    .delete-info p {
      font-size: 0.8125rem;
      color: var(--scion-text-muted, #64748b);
      margin: 0;
    }

    .delete-actions {
      flex-shrink: 0;
      display: flex;
      flex-direction: column;
      align-items: flex-end;
      gap: 0.75rem;
    }

    .checkbox-label {
      display: flex;
      align-items: center;
      gap: 0.5rem;
      font-size: 0.8125rem;
      color: var(--scion-text-muted, #64748b);
      cursor: pointer;
      user-select: none;
    }

    .checkbox-label input[type="checkbox"] {
      cursor: pointer;
    }

    .loading-state {
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      padding: 4rem 2rem;
      color: var(--scion-text-muted, #64748b);
    }

    .loading-state sl-spinner {
      font-size: 2rem;
      margin-bottom: 1rem;
    }

    .error-state {
      text-align: center;
      padding: 3rem 2rem;
      background: var(--scion-surface, #ffffff);
      border: 1px solid var(--sl-color-danger-200, #fecaca);
      border-radius: var(--scion-radius-lg, 0.75rem);
    }

    .error-state sl-icon {
      font-size: 3rem;
      color: var(--sl-color-danger-500, #ef4444);
      margin-bottom: 1rem;
    }

    .error-state h2 {
      font-size: 1.25rem;
      font-weight: 600;
      color: var(--scion-text, #1e293b);
      margin: 0 0 0.5rem 0;
    }

    .error-state p {
      color: var(--scion-text-muted, #64748b);
      margin: 0 0 1rem 0;
    }

    .error-details {
      font-family: var(--scion-font-mono, monospace);
      font-size: 0.875rem;
      background: var(--scion-bg-subtle, #f1f5f9);
      padding: 0.75rem 1rem;
      border-radius: var(--scion-radius, 0.5rem);
      color: var(--sl-color-danger-700, #b91c1c);
      margin-bottom: 1rem;
    }

    .done-footer {
      display: flex;
      justify-content: flex-start;
      margin-top: 1rem;
    }
  `;

  override connectedCallback(): void {
    super.connectedCallback();
    if (!this.groveId && typeof window !== 'undefined') {
      const match = window.location.pathname.match(/\/groves\/([^/]+)/);
      if (match) {
        this.groveId = match[1];
      }
    }
    void this.loadGrove();
    void this.loadTemplates();
    void this.loadMembersGroup();
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.stopSyncPolling();
  }

  private async loadGrove(): Promise<void> {
    this.loading = true;
    this.error = null;

    try {
      const response = await apiFetch(`/api/v1/groves/${this.groveId}`);

      if (!response.ok) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(
          errorData.message || `HTTP ${response.status}: ${response.statusText}`
        );
      }

      this.grove = (await response.json()) as Grove;
    } catch (err) {
      console.error('Failed to load grove:', err);
      this.error = err instanceof Error ? err.message : 'Failed to load grove';
    } finally {
      this.loading = false;
    }
  }

  private async loadTemplates(): Promise<void> {
    this.templatesLoading = true;
    try {
      const response = await apiFetch(
        `/api/v1/templates?scope=grove&groveId=${encodeURIComponent(this.groveId)}&status=active`
      );
      if (response.ok) {
        const data = (await response.json()) as { templates?: Template[] } | Template[];
        this.templates = Array.isArray(data) ? data : data.templates || [];
      }
    } catch (err) {
      console.error('Failed to load templates:', err);
    } finally {
      this.templatesLoading = false;
    }
  }

  private async loadMembersGroup(): Promise<void> {
    try {
      const response = await apiFetch(
        `/api/v1/groups?groveId=${encodeURIComponent(this.groveId)}&groupType=explicit&limit=10`
      );
      if (response.ok) {
        const data = (await response.json()) as { groups?: AdminGroup[] } | AdminGroup[];
        const groups = Array.isArray(data) ? data : data.groups || [];
        // Find the members group (slug pattern: grove:<slug>:members)
        this.membersGroup = groups.find((g) => g.slug?.endsWith(':members')) || null;
      }
    } catch (err) {
      console.error('Failed to load grove members group:', err);
    }
  }

  private async handleSyncTemplates(): Promise<void> {
    this.syncLoading = true;
    this.syncError = null;
    this.syncSuccess = null;

    try {
      const response = await apiFetch(`/api/v1/groves/${this.groveId}/sync-templates`, {
        method: 'POST',
      });

      if (!response.ok) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(errorData.message || `Failed to start template sync: HTTP ${response.status}`);
      }

      const data = (await response.json()) as { agentId: string; status: string };
      this.syncAgentId = data.agentId;
      this.startSyncPolling();
    } catch (err) {
      console.error('Failed to sync templates:', err);
      this.syncError = err instanceof Error ? err.message : 'Failed to sync templates';
      this.syncLoading = false;
    }
  }

  private startSyncPolling(): void {
    this.stopSyncPolling();
    this.syncPollTimer = setInterval(() => void this.pollSyncAgent(), 3000);
  }

  private stopSyncPolling(): void {
    if (this.syncPollTimer) {
      clearInterval(this.syncPollTimer);
      this.syncPollTimer = null;
    }
  }

  private async pollSyncAgent(): Promise<void> {
    if (!this.syncAgentId) return;

    try {
      const response = await apiFetch(`/api/v1/agents/${this.syncAgentId}`);
      if (!response.ok) {
        this.stopSyncPolling();
        this.syncError = 'Lost track of sync agent';
        this.syncLoading = false;
        return;
      }

      const agent = (await response.json()) as Agent;

      if (agent.phase === 'stopped' || agent.phase === 'completed') {
        this.stopSyncPolling();
        this.syncLoading = false;
        this.syncSuccess = 'Templates synced successfully.';
        void this.cleanupSyncAgent();
        void this.loadTemplates();
      } else if (agent.phase === 'error') {
        this.stopSyncPolling();
        this.syncLoading = false;
        this.syncError = 'Template sync agent encountered an error.';
        void this.cleanupSyncAgent();
      }
    } catch {
      this.stopSyncPolling();
      this.syncError = 'Failed to check sync status';
      this.syncLoading = false;
    }
  }

  private async cleanupSyncAgent(): Promise<void> {
    if (!this.syncAgentId) return;
    const agentId = this.syncAgentId;
    this.syncAgentId = null;
    try {
      await apiFetch(`/api/v1/agents/${agentId}`, { method: 'DELETE' });
    } catch {
      // Best-effort cleanup
    }
  }

  private async handleDeleteGrove(event?: MouseEvent): Promise<void> {
    const groveName = this.grove?.name || this.groveId;
    const agentWarning = this.deleteAlsoAgents
      ? '\n\nThis will also delete all agents in this grove.'
      : '';

    if (!event?.altKey && !confirm(`Are you sure you want to delete "${groveName}"?${agentWarning}\n\nThis action cannot be undone.`)) {
      return;
    }

    this.deleteLoading = true;

    try {
      const params = this.deleteAlsoAgents ? '?deleteAgents=true' : '';
      const response = await apiFetch(`/api/v1/groves/${this.groveId}${params}`, {
        method: 'DELETE',
      });

      if (!response.ok && response.status !== 204) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(errorData.message || `Failed to delete grove: HTTP ${response.status}`);
      }

      // Navigate back to groves list
      window.history.pushState({}, '', '/groves');
      window.dispatchEvent(new PopStateEvent('popstate'));
    } catch (err) {
      console.error('Failed to delete grove:', err);
      alert(err instanceof Error ? err.message : 'Failed to delete grove');
    } finally {
      this.deleteLoading = false;
    }
  }

  override render() {
    if (this.loading) {
      return this.renderLoading();
    }

    if (this.error || !this.grove) {
      return this.renderError();
    }

    return html`
      <a href="/groves/${this.groveId}" class="back-link">
        <sl-icon name="arrow-left"></sl-icon>
        Back to Grove
      </a>

      <div class="header">
        <sl-icon name="gear"></sl-icon>
        <h1>${this.grove.name} Settings</h1>
      </div>

      ${this.renderTemplatesSection()}

      ${this.membersGroup
        ? html`
            <scion-group-member-editor
              groupId=${this.membersGroup.id}
              ?readOnly=${!canAny(this.grove._capabilities, 'update', 'manage')}
              compact
              sectionTitle="Grove Members"
              sectionDescription="Users and groups who can create and manage agents in this grove."
            ></scion-group-member-editor>
          `
        : ''}

      ${canAny(this.grove._capabilities, 'update', 'manage') ? html`
        <scion-env-var-list
          scope="grove"
          scopeId=${this.groveId}
          apiBasePath="/api/v1/groves/${this.groveId}"
          compact
        ></scion-env-var-list>

        <scion-secret-list
          scope="grove"
          scopeId=${this.groveId}
          apiBasePath="/api/v1/groves/${this.groveId}"
          compact
        ></scion-secret-list>

        <scion-shared-dir-list
          groveId=${this.groveId}
          apiBasePath="/api/v1/groves/${this.groveId}"
        ></scion-shared-dir-list>
      ` : ''}

      ${can(this.grove._capabilities, 'delete') ? html`
        <div class="section danger-section">
          <h2>Danger Zone</h2>
          <p>Irreversible actions that affect this grove and its resources.</p>

          <div class="delete-area">
            <div class="delete-info">
              <h3>Delete this grove</h3>
              <p>
                Permanently remove this grove and its configuration.
                This action cannot be undone.
              </p>
            </div>
            <div class="delete-actions">
              <label class="checkbox-label">
                <input
                  type="checkbox"
                  .checked=${this.deleteAlsoAgents}
                  @change=${(e: Event) => {
                    this.deleteAlsoAgents = (e.target as HTMLInputElement).checked;
                  }}
                />
                Also delete all agents
              </label>
              <sl-button
                variant="danger"
                size="small"
                ?loading=${this.deleteLoading}
                ?disabled=${this.deleteLoading}
                @click=${(e: MouseEvent) => this.handleDeleteGrove(e)}
              >
                <sl-icon slot="prefix" name="trash"></sl-icon>
                Delete Grove
              </sl-button>
            </div>
          </div>
        </div>
      ` : html`
        <div class="section">
          <h2>Permissions</h2>
          <p>You don't have permission to modify this grove.</p>
        </div>
      `}

      <div class="done-footer">
        <sl-button
          variant="default"
          href="/groves/${this.groveId}"
        >
          <sl-icon slot="prefix" name="arrow-left"></sl-icon>
          Back to ${this.grove.name}
        </sl-button>
      </div>
    `;
  }

  private renderTemplatesSection() {
    return html`
      <div class="section">
        <div class="section-header">
          <div class="section-header-text">
            <h2>Templates</h2>
            <p>Grove-scoped agent templates synced to the Hub.</p>
          </div>
          ${canAny(this.grove!._capabilities, 'update', 'manage') ? html`
            <sl-button
              size="small"
              variant="default"
              ?loading=${this.syncLoading}
              ?disabled=${this.syncLoading}
              @click=${() => this.handleSyncTemplates()}
            >
              <sl-icon slot="prefix" name="arrow-repeat"></sl-icon>
              Load Templates
            </sl-button>
          ` : ''}
        </div>

        ${this.syncLoading ? html`
          <div class="sync-status syncing">
            <sl-spinner style="font-size: 0.875rem;"></sl-spinner>
            Syncing templates from grove...
          </div>
        ` : ''}

        ${this.syncError ? html`
          <div class="sync-status error">
            <sl-icon name="exclamation-triangle"></sl-icon>
            ${this.syncError}
          </div>
        ` : ''}

        ${this.syncSuccess ? html`
          <div class="sync-status success">
            <sl-icon name="check-circle"></sl-icon>
            ${this.syncSuccess}
          </div>
        ` : ''}

        ${this.templatesLoading && !this.syncLoading
          ? html`<div class="empty-templates"><sl-spinner></sl-spinner></div>`
          : this.templates.length > 0
            ? html`
                <div class="template-list">
                  ${this.templates.map(
                    (t) => html`
                      <div class="template-item">
                        <sl-icon name="file-earmark-code"></sl-icon>
                        <div class="template-info">
                          <div class="template-name">${t.displayName || t.name}</div>
                          ${t.description
                            ? html`<div class="template-meta">${t.description}</div>`
                            : ''}
                        </div>
                        ${t.harness
                          ? html`<span class="template-badge">${t.harness}</span>`
                          : ''}
                      </div>
                    `
                  )}
                </div>
              `
            : html`
                <div class="empty-templates">
                  <sl-icon name="file-earmark"></sl-icon>
                  <p>No grove templates synced yet.</p>
                  ${canAny(this.grove!._capabilities, 'update', 'manage')
                    ? html`<p>Use "Load Templates" to sync templates from the grove's filesystem.</p>`
                    : ''}
                </div>
              `}
      </div>
    `;
  }

  private renderLoading() {
    return html`
      <div class="loading-state">
        <sl-spinner></sl-spinner>
        <p>Loading settings...</p>
      </div>
    `;
  }

  private renderError() {
    return html`
      <a href="/groves/${this.groveId}" class="back-link">
        <sl-icon name="arrow-left"></sl-icon>
        Back to Grove
      </a>

      <div class="error-state">
        <sl-icon name="exclamation-triangle"></sl-icon>
        <h2>Failed to Load Settings</h2>
        <p>There was a problem loading this grove.</p>
        <div class="error-details">${this.error || 'Grove not found'}</div>
        <sl-button variant="primary" @click=${() => this.loadGrove()}>
          <sl-icon slot="prefix" name="arrow-clockwise"></sl-icon>
          Retry
        </sl-button>
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'scion-page-grove-settings': ScionPageGroveSettings;
  }
}
