import React, { Component } from 'react';
import { Link } from 'react-router-dom';
import { Button } from '@spatika/react';
import { RotateCw } from 'lucide-react';
import { ErrorState } from '../components/States';
import { PageStack, Panel } from '../components/Layout';

// A page that throws shows an inline error inside the shell, so navigation stays available.
export class AdminErrorBoundary extends Component {
  state = { error: null };
  static getDerivedStateFromError(error) { return { error }; }
  componentDidCatch(error, info) { console.error('Admin page crashed:', error, info); }
  componentDidUpdate(prev) {
    if (this.state.error && this.props.resetKeys?.some((key, i) => key !== prev.resetKeys?.[i])) this.setState({ error: null });
  }
  render() {
    const { error } = this.state;
    if (!error) return this.props.children;
    return <PageStack><Panel bodyless>
      <ErrorState centered title="This page hit an unexpected error" error={error}>
        <Button type="button" size="sm" variant="outline" onClick={() => this.setState({ error: null })} leadingIcon={<RotateCw size={14} />}>Try again</Button>
        <Button type="button" size="sm" variant="ghost" asChild><Link to="/admin">Back to the dashboard</Link></Button>
      </ErrorState>
    </Panel></PageStack>;
  }
}
