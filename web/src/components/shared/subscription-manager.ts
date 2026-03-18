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
 * Subscription Manager Component
 *
 * CRUD table + dialog for managing notification subscriptions.
 * Used on the grove detail page in compact mode.
 */

import { LitElement, html, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';

import { apiFetch } from '../../client/api.js';
import { resourceStyles } from './resource-styles.js';
import type { Subscription, SubscriptionScope } from '../../shared/types.js';

const DEFAULT_TRIGGERS = ['COMPLETED', 'WAITING_FOR_INPUT', 'LIMITS_EXCEEDED'];
const ALL_TRIGGERS = ['COMPLETED', 'WAITING_FOR_INPUT', 'LIMITS_EXCEEDED'];

@customElement('scion-subscription-manager')
export class ScionSubscriptionManager extends LitElement {
  @property() groveId = '';
  @property() agentId?: string;
  @property({ type: Boolean }) compact = false;

  @state() private loading = true;
  @state() private subscriptions: Subscription[] = [];
  @state() private error: string | null = null;

  // Create dialog
  @state() private dialogOpen = false;
  @state() private dialogScope: SubscriptionScope = 'grove';
  @state() private dialogAgentId = '';
  @state() private dialogTriggers: Set<string> = new Set(DEFAULT_TRIGGERS);
  @state() private dialogLoading = false;
  @state() private dialogError: string | null = null;

  // Delete state
  @state() private deletingId: string | null = null;

  static override styles = [resourceStyles];

  override connectedCallback(): void {
    super.connectedCallback();
    void this.loadSubscriptions();
  }

  private async loadSubscriptions(): Promise<void> {
    if (!this.groveId) return;
    this.loading = true;
    this.error = null;

    try {
      let url = `/api/v1/notifications/subscriptions?groveId=${encodeURIComponent(this.groveId)}`;
      if (this.agentId) {
        url += `&agentId=${encodeURIComponent(this.agentId)}`;
      }
      const response = await apiFetch(url);

      if (!response.ok) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(errorData.message || `HTTP ${response.status}: ${response.statusText}`);
      }

      const data = (await response.json()) as Subscription[] | { subscriptions?: Subscription[] };
      this.subscriptions = Array.isArray(data)
        ? data
        : (data as { subscriptions?: Subscription[] }).subscriptions || [];
    } catch (err) {
      console.error('Failed to load subscriptions:', err);
      this.error = err instanceof Error ? err.message : 'Failed to load subscriptions';
    } finally {
      this.loading = false;
    }
  }

  private openCreateDialog(): void {
    this.dialogScope = this.agentId ? 'agent' : 'grove';
    this.dialogAgentId = this.agentId || '';
    this.dialogTriggers = new Set(DEFAULT_TRIGGERS);
    this.dialogError = null;
    this.dialogOpen = true;
  }

  private closeDialog(): void {
    this.dialogOpen = false;
    this.dialogError = null;
  }

  private async handleCreate(e: Event): Promise<void> {
    e.preventDefault();
    this.dialogLoading = true;
    this.dialogError = null;

    try {
      const body: Record<string, unknown> = {
        scope: this.dialogScope,
        groveId: this.groveId,
        triggerActivities: [...this.dialogTriggers],
      };

      if (this.dialogScope === 'agent') {
        if (!this.dialogAgentId.trim()) {
          throw new Error('Agent ID is required for agent-scoped subscriptions');
        }
        body.agentId = this.dialogAgentId.trim();
      }

      const response = await apiFetch('/api/v1/notifications/subscriptions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });

      if (!response.ok) {
        const errorData = (await response.json().catch(() => ({}))) as {
          message?: string;
          error?: { message?: string };
        };
        throw new Error(
          errorData.error?.message || errorData.message || `HTTP ${response.status}`
        );
      }

      this.closeDialog();
      await this.loadSubscriptions();
    } catch (err) {
      this.dialogError = err instanceof Error ? err.message : 'Failed to create subscription';
    } finally {
      this.dialogLoading = false;
    }
  }

  private async handleDelete(id: string): Promise<void> {
    this.deletingId = id;

    try {
      const response = await apiFetch(
        `/api/v1/notifications/subscriptions/${encodeURIComponent(id)}`,
        { method: 'DELETE' }
      );

      if (!response.ok && response.status !== 204) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(errorData.message || `HTTP ${response.status}`);
      }

      await this.loadSubscriptions();
    } catch (err) {
      console.error('Failed to delete subscription:', err);
      this.error = err instanceof Error ? err.message : 'Failed to delete subscription';
    } finally {
      this.deletingId = null;
    }
  }

  private formatRelativeTime(dateString: string): string {
    try {
      const date = new Date(dateString);
      if (isNaN(date.getTime())) return dateString;
      const diffMs = Date.now() - date.getTime();
      const diffMinutes = Math.round(diffMs / (1000 * 60));
      const diffHours = Math.round(diffMs / (1000 * 60 * 60));
      const diffDays = Math.round(diffMs / (1000 * 60 * 60 * 24));

      const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' });

      if (Math.abs(diffMinutes) < 60) {
        return rtf.format(-diffMinutes, 'minute');
      } else if (Math.abs(diffHours) < 24) {
        return rtf.format(-diffHours, 'hour');
      } else {
        return rtf.format(-diffDays, 'day');
      }
    } catch {
      return dateString;
    }
  }

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

  override render() {
    if (this.compact) {
      return this.renderCompact();
    }
    return this.renderFull();
  }

  private renderCompact() {
    return html`
      <div class="section compact">
        <div class="section-header">
          <div class="section-header-info">
            <h2>Notification Subscriptions</h2>
            <p>Get notified when agents complete, need input, or exceed limits.</p>
          </div>
          <sl-button size="small" variant="default" @click=${this.openCreateDialog}>
            <sl-icon slot="prefix" name="bell"></sl-icon>
            Subscribe
          </sl-button>
        </div>

        ${this.loading
          ? html`<div class="section-loading"><sl-spinner></sl-spinner> Loading subscriptions...</div>`
          : this.error
            ? html`
                <div class="section-error">
                  ${this.error}
                  <sl-button size="small" @click=${() => this.loadSubscriptions()}>Retry</sl-button>
                </div>
              `
            : this.subscriptions.length === 0
              ? html`
                  <div class="empty-state">
                    <sl-icon name="bell-slash"></sl-icon>
                    <h3>No Subscriptions</h3>
                    <p>Subscribe to get notified about agent activity in this grove.</p>
                    <sl-button variant="primary" size="small" @click=${this.openCreateDialog}>
                      <sl-icon slot="prefix" name="bell"></sl-icon>
                      Subscribe
                    </sl-button>
                  </div>
                `
              : this.renderTable()}

        ${this.renderDialog()}
      </div>
    `;
  }

  private renderFull() {
    if (this.loading) {
      return html`
        <div class="loading-state">
          <sl-spinner></sl-spinner>
          <p>Loading subscriptions...</p>
        </div>
      `;
    }

    if (this.error) {
      return html`
        <div class="error-state">
          <sl-icon name="exclamation-triangle"></sl-icon>
          <h2>Failed to Load Subscriptions</h2>
          <div class="error-details">${this.error}</div>
          <sl-button variant="primary" @click=${() => this.loadSubscriptions()}>
            <sl-icon slot="prefix" name="arrow-clockwise"></sl-icon>
            Retry
          </sl-button>
        </div>
      `;
    }

    return html`
      <div class="list-header">
        <sl-button size="small" variant="primary" @click=${this.openCreateDialog}>
          <sl-icon slot="prefix" name="bell"></sl-icon>
          Subscribe
        </sl-button>
      </div>

      ${this.subscriptions.length === 0
        ? html`
            <div class="empty-state">
              <sl-icon name="bell-slash"></sl-icon>
              <h3>No Subscriptions</h3>
              <p>Subscribe to get notified about agent activity.</p>
            </div>
          `
        : this.renderTable()}

      ${this.renderDialog()}
    `;
  }

  private renderTable() {
    return html`
      <div class="table-container">
        <table>
          <thead>
            <tr>
              <th>Scope</th>
              <th>Target</th>
              <th class="hide-mobile">Triggers</th>
              <th class="hide-mobile">Created</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            ${this.subscriptions.map((sub) => this.renderRow(sub))}
          </tbody>
        </table>
      </div>
    `;
  }

  private renderRow(sub: Subscription) {
    const isDeleting = this.deletingId === sub.id;
    const target =
      sub.scope === 'grove' ? '(all agents)' : sub.agentId || '\u2014';
    const scopeIcon = sub.scope === 'grove' ? 'folder' : 'cpu';
    const triggers = sub.triggerActivities?.join(', ') || '\u2014';

    return html`
      <tr>
        <td>
          <span class="key-info">
            <sl-icon name=${scopeIcon} style="color: var(--scion-primary, #3b82f6); flex-shrink: 0;"></sl-icon>
            <span>${sub.scope}</span>
          </span>
        </td>
        <td><span class="meta-text">${target}</span></td>
        <td class="hide-mobile"><span class="meta-text">${triggers}</span></td>
        <td class="hide-mobile">
          <span class="meta-text">${this.formatRelativeTime(sub.createdAt)}</span>
        </td>
        <td class="actions-cell">
          <sl-icon-button
            name="trash"
            label="Delete"
            ?disabled=${isDeleting}
            @click=${() => this.handleDelete(sub.id)}
          ></sl-icon-button>
        </td>
      </tr>
    `;
  }

  private renderDialog() {
    return html`
      <sl-dialog
        label="Subscribe to Notifications"
        ?open=${this.dialogOpen}
        @sl-request-close=${this.closeDialog}
      >
        <form class="dialog-form" @submit=${this.handleCreate}>
          ${this.dialogError
            ? html`<div class="dialog-error">${this.dialogError}</div>`
            : nothing}

          ${!this.agentId
            ? html`
                <div class="radio-field">
                  <span class="radio-field-label">Scope</span>
                  <sl-radio-group
                    .value=${this.dialogScope}
                    @sl-change=${(e: Event) =>
                      (this.dialogScope = (e.target as HTMLInputElement).value as SubscriptionScope)}
                  >
                    <sl-radio-button value="grove">Entire Grove</sl-radio-button>
                    <sl-radio-button value="agent">Specific Agent</sl-radio-button>
                  </sl-radio-group>
                  <span class="radio-field-help">
                    ${this.dialogScope === 'grove'
                      ? 'Receive notifications for all agents in this grove.'
                      : 'Receive notifications for a specific agent only.'}
                  </span>
                </div>
              `
            : nothing}

          ${this.dialogScope === 'agent' && !this.agentId
            ? html`
                <sl-input
                  label="Agent ID"
                  placeholder="agent-uuid"
                  .value=${this.dialogAgentId}
                  @sl-input=${(e: Event) =>
                    (this.dialogAgentId = (e.target as HTMLInputElement).value)}
                  required
                ></sl-input>
              `
            : nothing}

          <div class="radio-field">
            <span class="radio-field-label">Trigger Activities</span>
            <div class="checkbox-group">
              ${ALL_TRIGGERS.map(
                (trigger) => html`
                  <label class="checkbox-label">
                    <input
                      type="checkbox"
                      .checked=${this.dialogTriggers.has(trigger)}
                      @change=${(e: Event) => {
                        const checked = (e.target as HTMLInputElement).checked;
                        const next = new Set(this.dialogTriggers);
                        if (checked) {
                          next.add(trigger);
                        } else {
                          next.delete(trigger);
                        }
                        this.dialogTriggers = next;
                      }}
                    />
                    <span class="checkbox-text">
                      <span>${this.triggerLabel(trigger)}</span>
                      <span class="checkbox-description">${this.triggerDescription(trigger)}</span>
                    </span>
                  </label>
                `
              )}
            </div>
          </div>
        </form>

        <sl-button slot="footer" variant="default" @click=${this.closeDialog}>Cancel</sl-button>
        <sl-button
          slot="footer"
          variant="primary"
          ?loading=${this.dialogLoading}
          ?disabled=${this.dialogTriggers.size === 0}
          @click=${this.handleCreate}
        >Subscribe</sl-button>
      </sl-dialog>
    `;
  }

  private triggerLabel(trigger: string): string {
    switch (trigger) {
      case 'COMPLETED':
        return 'Completed';
      case 'WAITING_FOR_INPUT':
        return 'Waiting for Input';
      case 'LIMITS_EXCEEDED':
        return 'Limits Exceeded';
      default:
        return trigger;
    }
  }

  private triggerDescription(trigger: string): string {
    switch (trigger) {
      case 'COMPLETED':
        return 'Agent finished its task.';
      case 'WAITING_FOR_INPUT':
        return 'Agent needs human input to continue.';
      case 'LIMITS_EXCEEDED':
        return 'Agent exceeded turn or model call limits.';
      default:
        return '';
    }
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'scion-subscription-manager': ScionSubscriptionManager;
  }
}
