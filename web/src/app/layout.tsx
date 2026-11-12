import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Nobody Inc",
  description: "Control room of the Nobody Inc agent organisation",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
