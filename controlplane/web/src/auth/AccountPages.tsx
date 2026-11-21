// Pages for the links Gombit's account email carries: password reset, email
// verification and email change confirmation, plus the form that requests a
// reset. They are reachable signed in or out, since a link is opened from a
// mailbox, and each posts its token to the control plane's Gombit auth API.
//
// Gombit mails a bare token unless the link's URL is configured, so the control
// plane sets GOMBIT_AUTH_PASSWORD_RESET_URL, GOMBIT_AUTH_EMAIL_VERIFICATION_URL
// and GOMBIT_AUTH_EMAIL_CHANGE_URL to these paths with "?token={token}" (see
// .env.example). The token is read once and dropped from the address bar, so it
// does not stay in the history or reach a later page.
//
// Verification and email change wait for a click: a mail scanner that opens a
// link must not spend its token.

import { useEffect, useState } from "react";
import type { FormEvent, ReactNode } from "react";
import { Link, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { ApiError, api } from "../api/client";
import { useSession } from "./session";

/** The paths of the account pages, served whatever the session. */
export const ACCOUNT_PATHS = ["/forgot-password", "/reset-password", "/verify-email", "/confirm-email"];

export function isAccountPath(pathname: string): boolean {
  return ACCOUNT_PATHS.includes(pathname.replace(/\/+$/, "") || "/");
}

/** AccountRoutes renders the account page for the current path. */
export function AccountRoutes() {
  return (
    <Routes>
      <Route path="/forgot-password" element={<ForgotPasswordView />} />
      <Route path="/reset-password" element={<ResetPasswordView />} />
      <Route path="/verify-email" element={<VerifyEmailView />} />
      <Route path="/confirm-email" element={<ConfirmEmailChangeView />} />
    </Routes>
  );
}

/**
 * useLinkToken returns the link's ?token= value, read on the first render, and
 * replaces the URL with the same path without its query.
 */
function useLinkToken(): string {
  const { pathname, search } = useLocation();
  const navigate = useNavigate();
  const [token] = useState(() => (new URLSearchParams(search).get("token") ?? "").trim());
  useEffect(() => {
    if (search) {
      navigate(pathname, { replace: true });
    }
  }, [pathname, search, navigate]);
  return token;
}

/**
 * errorText is the message to show for a failed call: the envelope's message
 * and, when it names fields, what is wrong with them (an invalid or expired
 * token comes back that way, as a 422 on the token field).
 */
function errorText(err: unknown, fallback: string): string {
  if (err instanceof ApiError) {
    const details = Object.values(err.fields ?? {}).flat();
    return details.length > 0 ? `${err.message.replace(/[.:]\s*$/, "")}: ${details.join("; ")}` : err.message;
  }
  return fallback;
}

function AccountCard({ title, label, children }: { title: string; label?: string; children: ReactNode }) {
  return (
    <main className="centered">
      <section className="card" aria-label={label ?? title}>
        <h1>{title}</h1>
        {children}
      </section>
    </main>
  );
}

function ErrorLine({ error }: { error: string | null }) {
  return error ? (
    <p role="alert" className="error">
      {error}
    </p>
  ) : null;
}

const signInLink = <Link to="/">Back to sign in</Link>;

function MissingToken({ title, retry }: { title: string; retry?: ReactNode }) {
  return (
    <AccountCard title={title}>
      <p role="alert" className="error">
        This link has no token. Open the link from the email again.
      </p>
      {retry}
      {signInLink}
    </AccountCard>
  );
}

/** ForgotPasswordView requests a reset link for an email address. */
export function ForgotPasswordView() {
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setPending(true);
    try {
      await api.post("/auth/password/forgot", { email });
      setSent(true);
    } catch (err) {
      setError(errorText(err, "The request failed"));
    } finally {
      setPending(false);
    }
  }

  if (sent) {
    // Gombit answers the same for every address, so the page cannot say more.
    return (
      <AccountCard title="Check your email">
        <p role="status">If an account uses {email}, we sent it a link to reset the password.</p>
        {signInLink}
      </AccountCard>
    );
  }
  return (
    <main className="centered">
      <form className="card" onSubmit={onSubmit} aria-label="Reset your password">
        <h1>Reset your password</h1>
        <label>
          Email
          <input
            type="email"
            name="email"
            autoComplete="username"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
          />
        </label>
        <ErrorLine error={error} />
        <button type="submit" disabled={pending}>
          {pending ? "Sending…" : "Send reset link"}
        </button>
        {signInLink}
      </form>
    </main>
  );
}

/** ResetPasswordView sets a new password with the token from a reset link. */
export function ResetPasswordView() {
  const token = useLinkToken();
  const { refresh } = useSession();
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    if (password !== confirm) {
      setError("The passwords do not match");
      return;
    }
    setPending(true);
    try {
      await api.post("/auth/password/reset", { token, password });
      setDone(true);
      // The reset signs every session out, this browser's included.
      await refresh().catch(() => undefined);
    } catch (err) {
      setError(errorText(err, "The password was not changed"));
    } finally {
      setPending(false);
    }
  }

  if (!token) {
    return <MissingToken title="Reset your password" retry={<Link to="/forgot-password">Request a new link</Link>} />;
  }
  if (done) {
    return (
      <AccountCard title="Password changed">
        <p role="status">Your password is changed and every session is signed out. Sign in with the new password.</p>
        <Link to="/">Sign in</Link>
      </AccountCard>
    );
  }
  return (
    <main className="centered">
      <form className="card" onSubmit={onSubmit} aria-label="Choose a new password">
        <h1>Choose a new password</h1>
        <label>
          New password
          <input
            type="password"
            name="password"
            autoComplete="new-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </label>
        <label>
          Repeat the new password
          <input
            type="password"
            name="confirm"
            autoComplete="new-password"
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            required
          />
        </label>
        <ErrorLine error={error} />
        <button type="submit" disabled={pending}>
          {pending ? "Saving…" : "Set password"}
        </button>
        <Link to="/forgot-password">Request a new link</Link>
      </form>
    </main>
  );
}

/**
 * TokenConfirmView posts a link's token to path when the user confirms, for
 * the links that need no other input.
 */
function TokenConfirmView(props: {
  title: string;
  intro: string;
  action: string;
  pendingAction: string;
  path: string;
  doneTitle: string;
  doneText: string;
}) {
  const token = useLinkToken();
  const { status, refresh } = useSession();
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function confirm() {
    setError(null);
    setPending(true);
    try {
      await api.post(props.path, { token });
      setDone(true);
      // The signed-in user's email or its verification changed.
      await refresh().catch(() => undefined);
    } catch (err) {
      setError(errorText(err, "The link did not work"));
    } finally {
      setPending(false);
    }
  }

  if (!token) {
    return <MissingToken title={props.title} />;
  }
  const home = <Link to="/">{status === "authenticated" ? "Continue to Forge" : "Sign in"}</Link>;
  if (done) {
    return (
      <AccountCard title={props.doneTitle}>
        <p role="status">{props.doneText}</p>
        {home}
      </AccountCard>
    );
  }
  return (
    <AccountCard title={props.title}>
      <p>{props.intro}</p>
      <ErrorLine error={error} />
      <button type="button" onClick={() => void confirm()} disabled={pending}>
        {pending ? props.pendingAction : props.action}
      </button>
      {home}
    </AccountCard>
  );
}

/** VerifyEmailView verifies an address with the token from its email. */
export function VerifyEmailView() {
  return (
    <TokenConfirmView
      title="Verify your email"
      intro="Confirm that this address is yours."
      action="Verify email"
      pendingAction="Verifying…"
      path="/auth/verify-email"
      doneTitle="Email verified"
      doneText="Your email address is verified."
    />
  );
}

/** ConfirmEmailChangeView makes the new address the account's email. */
export function ConfirmEmailChangeView() {
  return (
    <TokenConfirmView
      title="Confirm your new email"
      intro="Confirm the change to make this address your account's email."
      action="Confirm new email"
      pendingAction="Confirming…"
      path="/auth/email/change/confirm"
      doneTitle="Email changed"
      doneText="This address is now your account's email."
    />
  );
}
