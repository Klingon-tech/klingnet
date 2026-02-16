interface DetailRowProps {
  label: string;
  children: React.ReactNode;
  mono?: boolean;
}

export function DetailRow({ label, children, mono }: DetailRowProps) {
  return (
    <div className="flex items-start justify-between gap-4 py-2 border-b border-border last:border-0">
      <span className="text-sm text-muted-foreground shrink-0">{label}</span>
      <span className={`text-sm text-right ${mono ? 'font-mono' : ''} break-all min-w-0`}>
        {children}
      </span>
    </div>
  );
}
