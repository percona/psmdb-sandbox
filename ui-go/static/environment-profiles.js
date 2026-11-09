// Profiles reuse the existing validated configuration payload. Only the server's
// positive allowlist is persisted; this script never writes form data to storage.
document.addEventListener('DOMContentLoaded', () => {
  const form = document.getElementById('config-form');
  if (!form) return;
  const platform = form.dataset.platform;
  const select = document.getElementById('environment-starter');
  const status = document.getElementById('environment-profile-status');
  const name = document.getElementById('environment-profile-name');
  const enableProfile = document.getElementById('enable-environment-profile');
  const profileFields = document.getElementById('environment-profile-fields');
  const endpoint = '/api/environment-profiles';

  async function refresh(previous = select.value) {
    const data = await apiFetch(`${endpoint}?platform=${encodeURIComponent(platform)}`);
    const options = document.getElementById('environment-profile-options');
    options.replaceChildren(...(data.profiles || []).map(profile => new Option(profile.name, `profile:${profile.id}`)));
    if (Array.from(select.options).some(option => option.value === previous)) select.value = previous;
  }

  select.addEventListener('change', () => {
    if (!select.value) return;
    const [kind, id] = select.value.split(':');
    window.location.href = `/configure/${platform}?${kind}=${encodeURIComponent(id)}`;
  });
  enableProfile.addEventListener('change', () => {
    profileFields.hidden = !enableProfile.checked;
    if (enableProfile.checked) name.focus();
  });
  document.getElementById('save-environment-profile').addEventListener('click', () => {
    if (form.querySelector('[type="submit"]').disabled) return;
    const value = name.value.trim();
    if (!value) { status.textContent = 'Enter a profile name first.'; name.focus(); return; }
    // requestSubmit is synchronous until the existing async handler reaches its
    // fetch. Always clear the request marker, even if native validation blocks it.
    window.environmentProfileSaveName = value;
    try { form.requestSubmit(); } finally { window.environmentProfileSaveName = ''; }
  });
  window.environmentProfiles = {
    async saved(profile) {
      await refresh();
      select.value = `profile:${profile.id}`;
      status.textContent = `Profile “${profile.name}” saved without credentials or environment identity.`;
    },
  };
  const params = new URLSearchParams(window.location.search);
  const startingPoint = params.has('template') ? `template:${params.get('template')}`
    : params.has('profile') ? `profile:${params.get('profile')}` : '';
  refresh(startingPoint).catch(error => { status.textContent = `Profiles unavailable: ${error.message}`; });
});
