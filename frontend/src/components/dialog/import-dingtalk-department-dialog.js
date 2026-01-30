import React from 'react';
import PropTypes from 'prop-types';
import { Button } from 'reactstrap';
import Loading from '../loading';

const propTypes = {
  importDepartmentDialogToggle: PropTypes.func.isRequired,
  onImportDepartmentSubmit: PropTypes.func.isRequired,
  departmentsCount: PropTypes.number.isRequired,
  membersCount: PropTypes.number.isRequired,
  departmentName: PropTypes.string.isRequired,
};

class ImportDingtalkDepartmentDialog extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      isLoading : false,
    };
  }

  toggle = () => {
    this.props.importDepartmentDialogToggle(null);
  };

  handleSubmit = () => {
    this.props.onImportDepartmentSubmit();
    this.setState({ isLoading : true });
  };

  render() {
    const { departmentsCount, membersCount, departmentName } = this.props;
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title"><span>{'导入部门 '}</span><span className="op-target" title={departmentName}>{departmentName}</span></h5>
              <button type="button" className="close" onClick={this.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <p>{'将要导入 '}<strong>{departmentsCount}</strong>{' 个部门，其中包括 '}<strong>{membersCount}</strong>{' 个成员'}</p>
          {this.state.isLoading && <Loading/>}
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={this.toggle}>{'取消'}</Button>
          <Button color="primary" onClick={this.handleSubmit}>{'导入'}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

ImportDingtalkDepartmentDialog.propTypes = propTypes;

export default ImportDingtalkDepartmentDialog;
