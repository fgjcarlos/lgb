import { Route, Routes } from "react-router-dom";
import { Layout } from "@/components/Layout";
import { PageTitle } from "@/components/PageTitle";
import { ProtectedRoute } from "@/components/ProtectedRoute";
import { Login } from "@/pages/Login";
import { routes } from "@/router";

export function App() {
  return (
    <Routes>
      <Route
        path="/login"
        element={
          <PageTitle title="Sign in">
            <Login />
          </PageTitle>
        }
      />
      <Route
        element={
          <ProtectedRoute>
            <Layout />
          </ProtectedRoute>
        }
      >
        {routes.map((r) => (
          <Route
            key={r.path}
            path={r.path}
            element={
              <PageTitle title={r.label}>
                {r.requiredRole ? (
                  <ProtectedRoute requiredRole={r.requiredRole}>
                    {r.element}
                  </ProtectedRoute>
                ) : (
                  r.element
                )}
              </PageTitle>
            }
          />
        ))}
      </Route>
      <Route path="*" element={<div className="p-6">Not found.</div>} />
    </Routes>
  );
}
