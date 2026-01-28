import React from 'react';
import ReactDOM from 'react-dom';
import LibDecryptDialog from './components/dialog/lib-decrypt-dialog';
import './index.css';

// Simple test page to display the encrypted library password modal
class TestModalPage extends React.Component {
  render() {
    return (
      <div>
        <h1 style={{ padding: '20px' }}>Test Modal Page - Encrypted Library Password Dialog</h1>
        <LibDecryptDialog
          repoID="test-repo-id"
          onLibDecryptDialog={(success) => console.log('Modal closed, success:', success)}
        />
      </div>
    );
  }
}

ReactDOM.render(<TestModalPage />, document.getElementById('root'));
