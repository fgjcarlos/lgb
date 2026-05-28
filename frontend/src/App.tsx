import { Route, Routes } from "react-router-dom";
import { Layout } from "@/components/Layout";
import { ProtectedRoute } from "@/components/ProtectedRoute";
import { Login } from "@/pages/Login";
import { routes } from "@/router";

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
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
              r.requiredRole ? (
                <ProtectedRoute requiredRole={r.requiredRole}>
                  {r.element}
                </ProtectedRoute>
              ) : (
                r.element
              )
            }
          />
        ))}
      </Route>
      <Route path="*" element={<div className="p-6">Not found.</div>} />
    </Routes>
  );
}
