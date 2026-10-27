// app.js – shared utilities for MongoDB Deploy UI

const PLATFORM_SETTINGS_KEY = 'psmdbSandbox.platformSettings';
const DEFAULT_PLATFORM_SETTINGS = Object.freeze({
  aws: true,
  gcp: true,
  azure: true,
  chaos: false,
  docker: true,
});

function getPlatformSettings() {
  try {
    const raw = localStorage.getItem(PLATFORM_SETTINGS_KEY);
    if (!raw) return { ...DEFAULT_PLATFORM_SETTINGS };
    const parsed = JSON.parse(raw);
    return {
      ...DEFAULT_PLATFORM_SETTINGS,
      ...(parsed && typeof parsed === 'object' ? parsed : {}),
    };
  } catch (_) {
    return { ...DEFAULT_PLATFORM_SETTINGS };
  }
}

function savePlatformSettings(settings) {
  const merged = {
    ...DEFAULT_PLATFORM_SETTINGS,
    ...(settings && typeof settings === 'object' ? settings : {}),
  };
  localStorage.setItem(PLATFORM_SETTINGS_KEY, JSON.stringify(merged));
  return merged;
}

function isPlatformEnabled(platform) {
  return !!getPlatformSettings()[platform];
}

/**
 * Generic helper to fetch a URL and return parsed JSON response.
 * Handles non-JSON responses gracefully so callers always get a meaningful
 * error message (important for Safari which throws "The string did not match
 * the expected pattern." when JSON.parse fails on non-JSON bodies).
 */
async function apiFetch(url, method = 'GET', body = null) {
  const opts = {
    method,
    headers: { 'Content-Type': 'application/json' },
  };
  if (body !== null) opts.body = JSON.stringify(body);
  const resp = await fetch(url, opts);
  let data;
  try {
    data = await resp.json();
  } catch (_) {
    // Response body is not JSON (e.g. plain-text 404 from a proxy).
    throw new Error(`HTTP ${resp.status}: ${resp.statusText || 'unexpected response'}`);
  }
  if (!resp.ok) throw new Error(data.error || `HTTP ${resp.status}`);
  return data;
}

/**
 * Delete an environment from the index page.
 * @param {string} envId    - The environment ID.
 * @param {string} status   - Current environment status.
 * @param {string} platform - Platform ("docker", "aws", "gcp", "azure").
 */
async function deleteEnvironment(envId, status, platform) {
  // Statuses that imply resources are already gone.
  const alreadyDestroyed = new Set(['deleted', 'destroy_success']);
  const isDestroyed = alreadyDestroyed.has(status);
  const isDocker = platform === 'docker';

  let msg;
  if (isDestroyed) {
    const what = isDocker ? 'Docker containers have' : 'Cloud resources have';
    msg = `Remove the definition of environment "${envId}"? ${what} already been destroyed; you will no longer be able to redeploy it.`;
  } else {
    if (isDocker) {
      msg = `⚠️ WARNING: Docker containers for environment "${envId}" may still be running (status: ${status}).\n\nThis will only remove the environment from this UI — it will NOT stop or remove the containers.\n\nAre you sure you want to proceed?`;
    } else {
      msg = `⚠️ WARNING: Cloud resources for environment "${envId}" may still be running (status: ${status}).\n\nThis will only remove the environment from this UI — it will NOT destroy the cloud resources.\n\nAre you sure you want to proceed?`;
    }
  }
  if (!confirm(msg)) return;
  try {
    await apiFetch(`/api/environment/${envId}`, 'DELETE');
    location.reload();
  } catch (err) {
    alert('Error deleting: ' + err.message);
  }
}

