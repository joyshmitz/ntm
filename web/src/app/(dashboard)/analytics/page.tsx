'use client';

import { useState, useMemo } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { getAuthHeaders, getBaseUrl } from '@/lib/api/client';

// Types based on REST API
interface MetricsData {
  sessions?: SessionMetrics[];
  totals?: MetricsTotals;
  period?: string;
}

interface SessionMetrics {
  session: string;
  panes: number;
  agents: number;
  prompts_sent: number;
  tokens_used?: number;
  duration_minutes?: number;
  status: string;
}

interface MetricsTotals {
  sessions: number;
  panes: number;
  prompts: number;
  tokens?: number;
}

interface AnalyticsData {
  period: string;
  metrics?: MetricsData;
  sessions?: SessionAnalytics[];
}

interface SessionAnalytics {
  session: string;
  created_at: string;
  last_activity: string;
  agent_count: number;
  prompt_count: number;
}

interface Reservation {
  id: number;
  path_pattern: string;
  agent_name: string;
  exclusive: boolean;
  reason?: string;
  expires_ts: string;
  created_ts?: string;
}

interface ConflictInfo {
  path: string;
  holders: Array<{
    agent_name: string;
    exclusive: boolean;
    expires_ts: string;
  }>;
}

interface Snapshot {
  name: string;
  created_at?: string;
}

async function fetchJSON<T>(url: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${getBaseUrl()}${url}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...getAuthHeaders(),
      ...options?.headers,
    },
  });
  if (!res.ok) {
    const error = await res.json().catch(() => ({ message: res.statusText }));
    throw new Error(error.message || res.statusText);
  }
  const data = await res.json();
  return data.data || data;
}

// Period selector options
const PERIODS = [
  { value: '1h', label: '1 Hour' },
  { value: '6h', label: '6 Hours' },
  { value: '24h', label: '24 Hours' },
  { value: '7d', label: '7 Days' },
  { value: '30d', label: '30 Days' },
];

// Conflict Heatmap Component
function ConflictHeatmap({ reservations }: { reservations: Reservation[] }) {
  // Build a matrix of files × agents
  const { files, agents, matrix } = useMemo(() => {
    const fileSet = new Set<string>();
    const agentSet = new Set<string>();
    const conflicts = new Map<string, Map<string, Reservation[]>>();

    for (const r of reservations) {
      // Extract base path for grouping
      const basePath = r.path_pattern.split('/').slice(0, 3).join('/') || r.path_pattern;
      fileSet.add(basePath);
      agentSet.add(r.agent_name);

      if (!conflicts.has(basePath)) {
        conflicts.set(basePath, new Map());
      }
      const fileConflicts = conflicts.get(basePath)!;
      if (!fileConflicts.has(r.agent_name)) {
        fileConflicts.set(r.agent_name, []);
      }
      fileConflicts.get(r.agent_name)!.push(r);
    }

    return {
      files: Array.from(fileSet).sort(),
      agents: Array.from(agentSet).sort(),
      matrix: conflicts,
    };
  }, [reservations]);

  if (files.length === 0 || agents.length === 0) {
    return (
      <div className="text-center text-gray-500 py-8">
        No active reservations to display
      </div>
    );
  }

  // Color cell based on exclusive status and count
  const getCellStyle = (reservations: Reservation[] | undefined) => {
    if (!reservations || reservations.length === 0) {
      return 'bg-gray-800';
    }
    const hasExclusive = reservations.some((r) => r.exclusive);
    if (hasExclusive) {
      return 'bg-red-500/60 hover:bg-red-500/80';
    }
    return 'bg-yellow-500/40 hover:bg-yellow-500/60';
  };

  return (
    <div className="overflow-x-auto">
      <table className="min-w-full text-sm">
        <thead>
          <tr>
            <th className="p-2 text-left text-gray-400 font-normal">Path</th>
            {agents.map((agent) => (
              <th key={agent} className="p-2 text-center text-gray-400 font-normal min-w-[80px]">
                <span className="truncate block max-w-[80px]" title={agent}>
                  {agent}
                </span>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {files.map((file) => (
            <tr key={file} className="border-t border-gray-700">
              <td className="p-2 font-mono text-xs text-gray-300 max-w-[200px] truncate" title={file}>
                {file}
              </td>
              {agents.map((agent) => {
                const cellReservations = matrix.get(file)?.get(agent);
                return (
                  <td
                    key={`${file}-${agent}`}
                    className={`p-2 text-center ${getCellStyle(cellReservations)} transition-colors cursor-pointer`}
                    title={
                      cellReservations
                        ? `${cellReservations.length} reservation(s)\n${cellReservations.map((r) => `${r.exclusive ? 'Exclusive' : 'Shared'}: ${r.reason || 'No reason'}`).join('\n')}`
                        : 'No reservation'
                    }
                  >
                    {cellReservations && (
                      <span className="text-xs font-medium">
                        {cellReservations.some((r) => r.exclusive) ? '✕' : '○'}
                      </span>
                    )}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
      <div className="flex items-center gap-4 mt-4 text-xs text-gray-500">
        <div className="flex items-center gap-1">
          <span className="w-4 h-4 bg-red-500/60 rounded"></span>
          <span>Exclusive</span>
        </div>
        <div className="flex items-center gap-1">
          <span className="w-4 h-4 bg-yellow-500/40 rounded"></span>
          <span>Shared</span>
        </div>
        <div className="flex items-center gap-1">
          <span className="w-4 h-4 bg-gray-800 rounded border border-gray-600"></span>
          <span>None</span>
        </div>
      </div>
    </div>
  );
}

// Metric Card Component
function MetricCard({ label, value, subtext }: { label: string; value: string | number; subtext?: string }) {
  return (
    <div className="bg-gray-800 rounded-lg p-4 border border-gray-700">
      <div className="text-3xl font-bold text-gray-100">{value}</div>
      <div className="text-sm text-gray-400">{label}</div>
      {subtext && <div className="text-xs text-gray-500 mt-1">{subtext}</div>}
    </div>
  );
}

// Session Row Component
function SessionRow({ session }: { session: SessionMetrics }) {
  const statusColors: Record<string, string> = {
    active: 'text-green-400',
    idle: 'text-yellow-400',
    stopped: 'text-gray-400',
    error: 'text-red-400',
  };

  return (
    <tr className="border-t border-gray-700 hover:bg-gray-700/30">
      <td className="p-3 font-medium text-gray-200">{session.session}</td>
      <td className="p-3 text-gray-400">{session.panes}</td>
      <td className="p-3 text-gray-400">{session.agents}</td>
      <td className="p-3 text-gray-400">{session.prompts_sent}</td>
      <td className="p-3 text-gray-400">{session.tokens_used?.toLocaleString() || '-'}</td>
      <td className="p-3">
        <span className={statusColors[session.status] || 'text-gray-400'}>
          {session.status}
        </span>
      </td>
    </tr>
  );
}

// Main Analytics Page
export default function AnalyticsPage() {
  const queryClient = useQueryClient();
  const [period, setPeriod] = useState('24h');
  const [selectedSession, setSelectedSession] = useState<string>('');
  const [snapshotName, setSnapshotName] = useState('');
  const [showSnapshotModal, setShowSnapshotModal] = useState(false);

  // Metrics query
  const { data: metricsData, isLoading: metricsLoading } = useQuery({
    queryKey: ['metrics', period, selectedSession],
    queryFn: () => fetchJSON<MetricsData>(`/api/v1/metrics?period=${period}${selectedSession ? `&session=${selectedSession}` : ''}`),
    refetchInterval: 30000,
  });

  // Analytics query
  const daysMatch = period.match(/(\d+)d/);
  const days = daysMatch ? parseInt(daysMatch[1]) : 1;
  const { data: analyticsData } = useQuery({
    queryKey: ['analytics', days],
    queryFn: () => fetchJSON<AnalyticsData>(`/api/v1/analytics?days=${days}`),
    refetchInterval: 60000,
  });

  // Reservations query for heatmap
  const { data: reservationsData, isLoading: reservationsLoading } = useQuery({
    queryKey: ['reservations'],
    queryFn: () => fetchJSON<{ reservations: Reservation[] }>('/api/v1/reservations'),
    refetchInterval: 15000,
  });

  // Snapshots query
  const { data: snapshotsData } = useQuery({
    queryKey: ['metrics', 'snapshots'],
    queryFn: () => fetchJSON<{ snapshots: Snapshot[] }>('/api/v1/metrics/snapshots'),
  });

  // Save snapshot mutation
  const saveSnapshotMutation = useMutation({
    mutationFn: (name: string) => fetchJSON('/api/v1/metrics/snapshot', {
      method: 'POST',
      body: JSON.stringify({ name, session: selectedSession }),
    }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['metrics', 'snapshots'] });
      setShowSnapshotModal(false);
      setSnapshotName('');
      if (process.env.NODE_ENV === 'development') {
        console.log('[Analytics] Snapshot saved');
      }
    },
  });

  // Export mutation
  const exportMutation = useMutation({
    mutationFn: () => fetchJSON<{ report: unknown }>(`/api/v1/metrics/export?session=${selectedSession}`),
    onSuccess: (data) => {
      // Download as JSON
      const blob = new Blob([JSON.stringify(data.report, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `metrics-${new Date().toISOString().slice(0, 10)}.json`;
      a.click();
      URL.revokeObjectURL(url);
      if (process.env.NODE_ENV === 'development') {
        console.log('[Analytics] Metrics exported');
      }
    },
  });

  const reservations = reservationsData?.reservations || [];
  const sessions = metricsData?.sessions || [];
  const totals = metricsData?.totals;

  return (
    <div className="p-6 space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-100">Analytics</h1>
          <p className="text-sm text-gray-400">Performance metrics and conflict heatmap</p>
        </div>
        <div className="flex items-center gap-3">
          <select
            value={period}
            onChange={(e) => setPeriod(e.target.value)}
            className="bg-gray-700 border border-gray-600 rounded px-3 py-2 text-sm"
          >
            {PERIODS.map((p) => (
              <option key={p.value} value={p.value}>{p.label}</option>
            ))}
          </select>
          <button
            onClick={() => setShowSnapshotModal(true)}
            className="px-4 py-2 bg-purple-600 hover:bg-purple-500 rounded-lg font-medium transition-colors"
          >
            Save Snapshot
          </button>
          <button
            onClick={() => exportMutation.mutate()}
            disabled={exportMutation.isPending}
            className="px-4 py-2 bg-gray-600 hover:bg-gray-500 disabled:opacity-50 rounded-lg font-medium transition-colors"
          >
            {exportMutation.isPending ? 'Exporting...' : 'Export'}
          </button>
        </div>
      </div>

      {/* Stats Cards */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <MetricCard
          label="Sessions"
          value={totals?.sessions || sessions.length || 0}
          subtext={`in ${period}`}
        />
        <MetricCard
          label="Total Panes"
          value={totals?.panes || sessions.reduce((sum, s) => sum + s.panes, 0)}
        />
        <MetricCard
          label="Prompts Sent"
          value={totals?.prompts || sessions.reduce((sum, s) => sum + s.prompts_sent, 0)}
        />
        <MetricCard
          label="Active Reservations"
          value={reservations.length}
          subtext={`${reservations.filter((r) => r.exclusive).length} exclusive`}
        />
      </div>

      {/* Session Filter */}
      {sessions.length > 0 && (
        <div className="bg-gray-800 rounded-lg p-4 border border-gray-700">
          <label className="text-sm text-gray-400 block mb-2">Filter by Session</label>
          <select
            value={selectedSession}
            onChange={(e) => setSelectedSession(e.target.value)}
            className="bg-gray-700 border border-gray-600 rounded px-3 py-2 w-full md:w-auto"
          >
            <option value="">All Sessions</option>
            {sessions.map((s) => (
              <option key={s.session} value={s.session}>{s.session}</option>
            ))}
          </select>
        </div>
      )}

      {/* Sessions Table */}
      <div className="bg-gray-800 rounded-lg border border-gray-700">
        <div className="p-4 border-b border-gray-700">
          <h2 className="text-lg font-semibold text-gray-100">Session Metrics</h2>
        </div>
        <div className="overflow-x-auto">
          {metricsLoading ? (
            <div className="text-center text-gray-500 py-8">Loading metrics...</div>
          ) : sessions.length === 0 ? (
            <div className="text-center text-gray-500 py-8">No session data available</div>
          ) : (
            <table className="min-w-full">
              <thead className="bg-gray-700/50">
                <tr>
                  <th className="p-3 text-left text-gray-400 font-medium">Session</th>
                  <th className="p-3 text-left text-gray-400 font-medium">Panes</th>
                  <th className="p-3 text-left text-gray-400 font-medium">Agents</th>
                  <th className="p-3 text-left text-gray-400 font-medium">Prompts</th>
                  <th className="p-3 text-left text-gray-400 font-medium">Tokens</th>
                  <th className="p-3 text-left text-gray-400 font-medium">Status</th>
                </tr>
              </thead>
              <tbody>
                {sessions
                  .filter((s) => !selectedSession || s.session === selectedSession)
                  .map((session) => (
                    <SessionRow key={session.session} session={session} />
                  ))}
              </tbody>
            </table>
          )}
        </div>
      </div>

      {/* Conflict Heatmap */}
      <div className="bg-gray-800 rounded-lg border border-gray-700">
        <div className="p-4 border-b border-gray-700">
          <h2 className="text-lg font-semibold text-gray-100">Conflict Heatmap</h2>
          <p className="text-sm text-gray-400 mt-1">File reservations by agent (files × agents matrix)</p>
        </div>
        <div className="p-4">
          {reservationsLoading ? (
            <div className="text-center text-gray-500 py-8">Loading reservations...</div>
          ) : (
            <ConflictHeatmap reservations={reservations} />
          )}
        </div>
      </div>

      {/* Reservations List */}
      {reservations.length > 0 && (
        <div className="bg-gray-800 rounded-lg border border-gray-700">
          <div className="p-4 border-b border-gray-700">
            <h2 className="text-lg font-semibold text-gray-100">Active Reservations</h2>
          </div>
          <div className="p-4 space-y-2 max-h-[300px] overflow-y-auto">
            {reservations.map((r) => (
              <div
                key={r.id}
                className={`p-3 rounded-lg border ${r.exclusive ? 'bg-red-500/10 border-red-500/30' : 'bg-yellow-500/10 border-yellow-500/30'}`}
              >
                <div className="flex items-center justify-between">
                  <div>
                    <span className="font-mono text-sm text-gray-300">{r.path_pattern}</span>
                    <span className="ml-2 text-xs text-gray-500">by {r.agent_name}</span>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className={`text-xs px-2 py-0.5 rounded ${r.exclusive ? 'bg-red-500/20 text-red-300' : 'bg-yellow-500/20 text-yellow-300'}`}>
                      {r.exclusive ? 'Exclusive' : 'Shared'}
                    </span>
                    <span className="text-xs text-gray-500">
                      expires {new Date(r.expires_ts).toLocaleTimeString()}
                    </span>
                  </div>
                </div>
                {r.reason && (
                  <div className="text-xs text-gray-500 mt-1">{r.reason}</div>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Snapshot Modal */}
      {showSnapshotModal && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-gray-800 rounded-lg p-6 w-full max-w-md border border-gray-700">
            <h3 className="text-lg font-semibold text-gray-100 mb-4">Save Metrics Snapshot</h3>
            <input
              type="text"
              value={snapshotName}
              onChange={(e) => setSnapshotName(e.target.value)}
              placeholder="Snapshot name (e.g., baseline, before-refactor)"
              className="w-full bg-gray-700 border border-gray-600 rounded px-3 py-2 mb-4"
              autoFocus
            />
            <div className="flex justify-end gap-3">
              <button
                onClick={() => {
                  setShowSnapshotModal(false);
                  setSnapshotName('');
                }}
                className="px-4 py-2 bg-gray-600 hover:bg-gray-500 rounded-lg"
              >
                Cancel
              </button>
              <button
                onClick={() => snapshotName && saveSnapshotMutation.mutate(snapshotName)}
                disabled={!snapshotName || saveSnapshotMutation.isPending}
                className="px-4 py-2 bg-purple-600 hover:bg-purple-500 disabled:opacity-50 rounded-lg"
              >
                {saveSnapshotMutation.isPending ? 'Saving...' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
