import React from 'react';
import PropTypes from 'prop-types';
import { gettext } from '../../utils/constants';
import { Button } from 'reactstrap';

const propTypes = {
  toggleCancel: PropTypes.func.isRequired,
  handleSubmit: PropTypes.func.isRequired,
};

class WikiDeleteDialog extends React.Component {

  toggle = () => {
    this.props.toggleCancel();
  };

  render() {
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Unpublish Library')}</h5>
              <button type="button" className="close" onClick={this.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <p>{gettext('Are you sure you want to unpublish this library?')}</p>
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={this.toggle}>{gettext('Cancel')}</Button>
          <Button color="primary" onClick={this.props.handleSubmit}>{gettext('Unpublish')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

WikiDeleteDialog.propTypes = propTypes;

export default WikiDeleteDialog;
