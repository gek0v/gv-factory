// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { useState, useEffect } from 'react';
import {
  fetchSkills,
  fetchTools,
  createAgentTemplate,
  type Skill,
  type Tool,
  type AgentTemplate,
} from '../../api/registry';
import {
  BotIcon,
  XIcon,
  PlusIcon,
  CheckIcon,
  ZapIcon,
  AlertCircleIcon,
} from '../icons/Icons';

export interface NewTemplateModalProps {
  isOpen: boolean;
  onClose: () => void;
  onTemplateCreated?: (template: AgentTemplate) => void;
}

const LLM_MODELS = [
  { id: 'claude-3-7-sonnet', label: 'Claude 3.7 Sonnet (Anthropic - Recommended)' },
  { id: 'claude-3-5-sonnet', label: 'Claude 3.5 Sonnet (Anthropic)' },
  { id: 'deepseek-r1', label: 'DeepSeek R1 (Reasoning)' },
  { id: 'gpt-4o', label: 'GPT-4o (OpenAI)' },
  { id: 'o3-mini', label: 'o3-mini (OpenAI Reasoning)' },
  { id: 'gemini-1.5-pro', label: 'Gemini 1.5 Pro (Google)' },
];

const ROLES = [
  { id: 'builder_tdd', label: 'TDD Builder (Red-Green-Refactor)' },
  { id: 'spec_writer', label: 'Spec Writer (SDD Architecture)' },
  { id: 'reviewer', label: 'Code Reviewer' },
  { id: 'security_reviewer', label: 'Security Reviewer (OWASP/SAST)' },
  { id: 'architecture_reviewer', label: 'Architecture Reviewer' },
  { id: 'approver_gate', label: 'Approver Gatekeeper' },
  { id: 'bug_hunter', label: 'Bug Hunter (Diagnostics)' },
  { id: 'refactor_specialist', label: 'Refactor Specialist' },
  { id: 'evaluator', label: 'Quality Evaluator' },
];

const THINKING_LEVELS = [
  { id: 'off', label: 'Off' },
  { id: 'low', label: 'Low' },
  { id: 'medium', label: 'Med' },
  { id: 'high', label: 'High' },
  { id: 'max', label: 'Max' },
];

export const NewTemplateModal: React.FC<NewTemplateModalProps> = ({
  isOpen,
  onClose,
  onTemplateCreated,
}) => {
  const [id, setId] = useState('');
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [role, setRole] = useState('builder_tdd');
  const [model, setModel] = useState('claude-3-7-sonnet');
  const [thinking, setThinking] = useState('high');
  const [systemPrompt, setSystemPrompt] = useState('');

  const [availableSkills, setAvailableSkills] = useState<Skill[]>([]);
  const [availableTools, setAvailableTools] = useState<Tool[]>([]);
  const [selectedSkills, setSelectedSkills] = useState<string[]>(['tdd-workflow']);
  const [selectedTools, setSelectedTools] = useState<string[]>(['bash_exec', 'git_read', 'file_writer']);

  const [isLoading, setIsLoading] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  useEffect(() => {
    if (!isOpen) return;

    let isMounted = true;
    setIsLoading(true);
    setErrorMessage(null);

    Promise.all([fetchSkills(), fetchTools()])
      .then(([skills, tools]) => {
        if (!isMounted) return;
        setAvailableSkills(skills || []);
        setAvailableTools(tools || []);
      })
      .catch((err) => {
        if (!isMounted) return;
        console.warn('Failed to load skills/tools catalog:', err);
      })
      .finally(() => {
        if (isMounted) setIsLoading(false);
      });

    return () => {
      isMounted = false;
    };
  }, [isOpen]);

  // Auto-slugify name to ID if ID is empty or user is typing name
  const handleNameChange = (val: string) => {
    setName(val);
    if (!id || id.startsWith('tpl-')) {
      const slug = val
        .toLowerCase()
        .replace(/[^a-z0-9_-]/g, '_')
        .replace(/_+/g, '_')
        .slice(0, 32);
      setId(slug ? `tpl_${slug}` : '');
    }
  };

  const toggleSkill = (skillId: string) => {
    setSelectedSkills((prev) =>
      prev.includes(skillId) ? prev.filter((s) => s !== skillId) : [...prev, skillId]
    );
  };

  const toggleTool = (toolId: string) => {
    setSelectedTools((prev) =>
      prev.includes(toolId) ? prev.filter((t) => t !== toolId) : [...prev, toolId]
    );
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErrorMessage(null);

    const trimmedName = name.trim();
    if (!trimmedName) {
      setErrorMessage('Template name is required.');
      return;
    }

    const trimmedId = (id.trim() || `tpl_${Date.now()}`).toLowerCase().replace(/[^a-z0-9_-]/g, '_');
    if (!trimmedId) {
      setErrorMessage('Template ID is required.');
      return;
    }

    setIsSubmitting(true);
    try {
      const newTemplate: AgentTemplate = {
        id: trimmedId,
        name: trimmedName,
        description: description.trim() || `${trimmedName} persona`,
        role,
        model,
        thinking: thinking !== 'off',
        prompt: systemPrompt.trim(),
        system_prompt: systemPrompt.trim(),
        skills: selectedSkills,
        tools: selectedTools,
        plugins: [],
      };

      const created = await createAgentTemplate(newTemplate);
      if (onTemplateCreated) {
        onTemplateCreated(created);
      }
      onClose();
    } catch (err: any) {
      setErrorMessage(err?.message || 'Failed to save agent template.');
    } finally {
      setIsSubmitting(false);
    }
  };

  if (!isOpen) return null;

  return (
    <div
      className="obsidian-glass-backdrop"
      onClick={onClose}
      role="dialog"
      aria-modal="true"
      aria-labelledby="new-template-title"
      data-testid="new-template-modal-backdrop"
    >
      <div
        className="obsidian-glass-modal"
        onClick={(e) => e.stopPropagation()}
        data-testid="new-template-modal"
      >
        {/* Modal Header */}
        <div className="obsidian-modal-header">
          <div className="obsidian-modal-title-group">
            <span className="obsidian-icon-badge">
              <BotIcon size={20} />
            </span>
            <div>
              <h2 id="new-template-title" className="obsidian-modal-title">
                Create Agent Template
              </h2>
              <p className="obsidian-modal-subtitle">
                Design and persist a reusable autonomous agent persona in Obsidian Glass
              </p>
            </div>
          </div>
          <button
            type="button"
            className="obsidian-close-btn"
            onClick={onClose}
            aria-label="Close modal"
            data-testid="close-modal-btn"
          >
            <XIcon size={18} />
          </button>
        </div>

        {/* Modal Body / Form */}
        <form onSubmit={handleSubmit} className="obsidian-modal-form">
          <div className="obsidian-modal-scroll">
            {errorMessage && (
              <div className="obsidian-alert-error" role="alert">
                <AlertCircleIcon size={16} />
                <span>{errorMessage}</span>
              </div>
            )}

            {/* Grid 1: Name and ID */}
            <div className="obsidian-form-row obsidian-form-grid-2">
              <div className="obsidian-field-group">
                <label htmlFor="tpl-name" className="obsidian-label">
                  Template Name <span className="obsidian-required">*</span>
                </label>
                <input
                  id="tpl-name"
                  type="text"
                  className="obsidian-input"
                  placeholder="e.g. Cloud Security Sentinel"
                  value={name}
                  onChange={(e) => handleNameChange(e.target.value)}
                  required
                  autoFocus
                  data-testid="input-template-name"
                />
              </div>

              <div className="obsidian-field-group">
                <label htmlFor="tpl-id" className="obsidian-label">
                  Template ID <span className="obsidian-required">*</span>
                </label>
                <input
                  id="tpl-id"
                  type="text"
                  className="obsidian-input font-mono"
                  placeholder="e.g. tpl_security_sentinel"
                  value={id}
                  onChange={(e) => setId(e.target.value)}
                  required
                  data-testid="input-template-id"
                />
              </div>
            </div>

            {/* Description */}
            <div className="obsidian-field-group">
              <label htmlFor="tpl-desc" className="obsidian-label">
                Description
              </label>
              <input
                id="tpl-desc"
                type="text"
                className="obsidian-input"
                placeholder="Brief summary of this agent's responsibilities and mandate"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                data-testid="input-template-desc"
              />
            </div>

            {/* Grid 2: Role and Model */}
            <div className="obsidian-form-row obsidian-form-grid-2">
              <div className="obsidian-field-group">
                <label htmlFor="tpl-role" className="obsidian-label">
                  Persona Role
                </label>
                <select
                  id="tpl-role"
                  className="obsidian-select"
                  value={role}
                  onChange={(e) => setRole(e.target.value)}
                  data-testid="select-template-role"
                >
                  {ROLES.map((r) => (
                    <option key={r.id} value={r.id}>
                      {r.label}
                    </option>
                  ))}
                </select>
              </div>

              <div className="obsidian-field-group">
                <label htmlFor="tpl-model" className="obsidian-label">
                  LLM Model
                </label>
                <select
                  id="tpl-model"
                  className="obsidian-select font-mono"
                  value={model}
                  onChange={(e) => setModel(e.target.value)}
                  data-testid="select-template-model"
                >
                  {LLM_MODELS.map((m) => (
                    <option key={m.id} value={m.id}>
                      {m.label}
                    </option>
                  ))}
                </select>
              </div>
            </div>

            {/* Thinking / Reasoning Effort */}
            <div className="obsidian-field-group">
              <label className="obsidian-label">
                Reasoning / Thinking Effort
              </label>
              <div className="obsidian-thinking-chips">
                {THINKING_LEVELS.map((lvl) => {
                  const isActive = thinking === lvl.id;
                  return (
                    <button
                      key={lvl.id}
                      type="button"
                      className={`obsidian-thinking-chip ${isActive ? 'active' : ''}`}
                      onClick={() => setThinking(lvl.id)}
                      data-testid={`thinking-btn-${lvl.id}`}
                    >
                      <ZapIcon size={12} />
                      <span>{lvl.label}</span>
                    </button>
                  );
                })}
              </div>
            </div>

            {/* Skills Dynamic Chips */}
            <div className="obsidian-field-group">
              <div className="obsidian-chip-header">
                <label className="obsidian-label">
                  Skills ({selectedSkills.length} selected)
                </label>
                <span className="obsidian-subtext">Click chips to toggle</span>
              </div>
              <div className="obsidian-chips-container" data-testid="skills-chips-container">
                {availableSkills.length === 0 && !isLoading && (
                  <span className="obsidian-empty-text">No skills discovered in registry</span>
                )}
                {availableSkills.map((sk) => {
                  const isSelected = selectedSkills.includes(sk.id);
                  return (
                    <button
                      key={sk.id}
                      type="button"
                      className={`obsidian-chip obsidian-chip-skill ${isSelected ? 'selected' : ''}`}
                      onClick={() => toggleSkill(sk.id)}
                      title={sk.description}
                      data-testid={`skill-chip-${sk.id}`}
                    >
                      {isSelected ? <CheckIcon size={12} /> : <PlusIcon size={12} />}
                      <span>{sk.name || sk.id}</span>
                    </button>
                  );
                })}
              </div>
            </div>

            {/* Tools Dynamic Chips */}
            <div className="obsidian-field-group">
              <div className="obsidian-chip-header">
                <label className="obsidian-label">
                  Tools & MCPs ({selectedTools.length} selected)
                </label>
                <span className="obsidian-subtext">Click chips to toggle</span>
              </div>
              <div className="obsidian-chips-container" data-testid="tools-chips-container">
                {availableTools.length === 0 && !isLoading && (
                  <span className="obsidian-empty-text">No tools discovered in registry</span>
                )}
                {availableTools.map((tool) => {
                  const isSelected = selectedTools.includes(tool.id);
                  return (
                    <button
                      key={tool.id}
                      type="button"
                      className={`obsidian-chip obsidian-chip-tool font-mono ${isSelected ? 'selected' : ''}`}
                      onClick={() => toggleTool(tool.id)}
                      title={tool.description}
                      data-testid={`tool-chip-${tool.id}`}
                    >
                      {isSelected ? <CheckIcon size={12} /> : <PlusIcon size={12} />}
                      <span>{tool.name || tool.id}</span>
                      {tool.riskLevel === 'sensitive' && <span className="obsidian-tag-risk">SENS</span>}
                    </button>
                  );
                })}
              </div>
            </div>

            {/* System Prompt */}
            <div className="obsidian-field-group">
              <label htmlFor="tpl-prompt" className="obsidian-label">
                System Prompt Persona
              </label>
              <textarea
                id="tpl-prompt"
                rows={4}
                className="obsidian-textarea font-mono"
                placeholder="You are an autonomous engineering persona specialized in..."
                value={systemPrompt}
                onChange={(e) => setSystemPrompt(e.target.value)}
                data-testid="textarea-template-prompt"
              />
              <span className="obsidian-subtext">
                Injects initial behavioral directives, domain constraints, and workflow instructions.
              </span>
            </div>
          </div>

          {/* Modal Footer / Actions */}
          <div className="obsidian-modal-footer">
            <button
              type="button"
              className="obsidian-btn obsidian-btn-cancel"
              onClick={onClose}
              disabled={isSubmitting}
            >
              Cancel
            </button>
            <button
              type="submit"
              className="obsidian-btn obsidian-btn-primary"
              disabled={isSubmitting || !name.trim()}
              data-testid="submit-template-btn"
            >
              {isSubmitting ? (
                <>
                  <span className="obsidian-spinner" />
                  <span>Saving...</span>
                </>
              ) : (
                <>
                  <PlusIcon size={14} />
                  <span>Create Template</span>
                </>
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
};
