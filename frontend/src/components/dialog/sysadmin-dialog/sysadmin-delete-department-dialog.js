import React from 'react';
import PropTypes from 'prop-types';
import { Button } from 'reactstrap';
import { gettext } from '../../../utils/constants';
import { seafileAPI } from '../../../utils/seafile-api';
import { Utils } from '../../../utils/utils';
import toaster from '../../../components/toast';

const propTypes = {
  group: PropTypes.object.isRequired,
  toggle: PropTypes.func.isRequired,
  onDeleteDepartment: PropTypes.func.isRequired
};

class DeleteDepartmentDialog extends React.Component {

  constructor(props) {
    super(props);
  }

  deleteDepart = () => {
    this.props.toggle();
    const { group } = this.props;
    seafileAPI.sysAdminDeleteDepartment(group.id).then((res) => {
      this.props.onDeleteDepartment(group.id);
    }).catch(error => {
      let errMessage = Utils.getErrorMsg(error);
      toaster.danger(errMessage);
    });
  };

  render() {
    const { group } = this.props;

    let tipMessage = gettext('Are you sure you want to delete {placeholder} ?');
    tipMessage = tipMessage.replace('{placeholder}', '<span class="op-target">' + Utils.HTMLescape(group.name) + '</span>');
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Delete Department')}</h5>
              <button type="button" className="close" onClick={this.props.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <p dangerouslySetInnerHTML={{__html: tipMessage}}></p>
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={this.props.toggle}>{gettext('Cancel')}</Button>
          <Button color="primary" onClick={this.deleteDepart}>{gettext('Delete')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

DeleteDepartmentDialog.propTypes = propTypes;

export default DeleteDepartmentDialog;
