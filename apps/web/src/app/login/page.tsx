import { LoginForm } from "./login-form";

export default function LoginPage() {
  return (
    <main className="login">
      <div className="login__plaque">
        <p className="login__brand">Stock Agents</p>
        <p className="login__tagline">EOD paper desk</p>
        <h1 className="login__title">Login</h1>
        <LoginForm />
      </div>
    </main>
  );
}
